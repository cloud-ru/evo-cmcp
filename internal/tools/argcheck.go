// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

// checkedArgs refuses, before the handler, the arguments CheckArgs refuses.
func checkedArgs(name string, schema map[string]any, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		if err := checkArgs(name, schema, args); err != nil {
			return errResult(err), true
		}
		return next(ctx, args)
	}
}

func CheckArgs(t Tool, args map[string]any) error {
	return checkArgs(t.Name, t.InputSchema, args)
}

func checkArgs(name string, schema map[string]any, args map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	unknown := 0
	for k := range args {
		// confirmed on a read-only tool is harmless: agents send it
		// after an approval they got for another call.
		if _, ok := props[k]; !ok && k != "confirmed" {
			unknown++
		}
	}
	if unknown > 0 {
		known := make([]string, 0, len(props))
		for k := range props {
			known = append(known, k)
		}
		sort.Strings(known)
		hint := i18n.Tf("%s takes: no arguments.", "%s не принимает аргументов.", name)
		if len(known) > 0 {
			hint = i18n.Tf("%s takes: %s.", "Аргументы %s: %s.", name, strings.Join(known, ", "))
		}
		return clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%d unknown argument(s)", "неизвестных аргументов: %d", unknown),
			hint)
	}
	for k, v := range args {
		p, _ := props[k].(map[string]any)
		typ, _ := p["type"].(string)
		if v != nil && !scalarOfType(typ, v) {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("%s must be %s, got %s", "%s должен быть %s, получено: %s", k, typeName(typ), ValueKind(v)),
				i18n.Tf("Pass %s as a JSON %s (see the tool's inputSchema).",
					"Передайте %s как JSON %s (см. inputSchema инструмента).", k, typ))
		}
	}
	for k, v := range args {
		p, _ := props[k].(map[string]any)
		if err := ruleArg(k, p, v); err != nil {
			return err
		}
	}
	for k, v := range args {
		p, _ := props[k].(map[string]any)
		if _, ruled := p["minItems"]; ruled {
			list, err := ruledList(k, p, v)
			if err != nil {
				return err
			}
			if list != nil {
				args[k] = list
			}
			continue
		}
		if s, isStr := v.(string); isStr {
			canon, err := enumValue(k, p, s)
			if err != nil {
				return err
			}
			args[k] = canon
		}
	}
	return nil
}

// enumValue is s in the spelling of the enum of its schema p (any case,
// no spaces around); s itself when p has no enum or s is empty.
func enumValue(k string, p map[string]any, s string) (string, error) {
	enum, _ := p["enum"].([]any)
	if len(enum) == 0 || s == "" {
		return s, nil
	}
	var allowed []string
	for _, e := range enum {
		es, _ := e.(string)
		allowed = append(allowed, es)
		// Spaces around the value are not part of it (" public ").
		if strings.EqualFold(es, strings.TrimSpace(s)) {
			return es, nil
		}
	}
	return "", clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%s: the value is not one of %s", "%s: значение не входит в список: %s", k, strings.Join(allowed, ", ")),
		i18n.Tf("Pass %s as one of: %s.", "Допустимые значения %s: %s.", k, strings.Join(allowed, ", ")))
}

func ruledList(k string, p map[string]any, v any) ([]any, error) {
	min, _ := p["minItems"].(int)
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s must be a list, got %s", "%s должен быть списком, получено: %s", k, ValueKind(v)),
			i18n.Tf("Pass %s as a JSON array of strings.", "Передайте %s как JSON-массив строк.", k))
	}
	if len(list) < min {
		return nil, ruleErr(k, p, i18n.Tf("%s is empty", "%s пустой", k),
			i18n.Tf("Give %s a value or drop the argument: an empty list is not read as «not set».",
				"Задайте %s значение или уберите аргумент: пустой список не читается как «не задано».", k))
	}
	items, _ := p["items"].(map[string]any)
	out := make([]any, len(list))
	for i, it := range list {
		name := fmt.Sprintf("%s[%d]", k, i)
		s, ok := it.(string)
		if !ok {
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("%s must be a string, got %s", "%s должен быть строкой, получено: %s", name, ValueKind(it)),
				i18n.Tf("Pass %s as a JSON array of strings.", "Передайте %s как JSON-массив строк.", k))
		}
		if strings.TrimSpace(s) == "" {
			return nil, ruleErr(k, p, i18n.Tf("%s is empty", "%s пустой", name),
				i18n.Tf("Take the empty value out of %s: it is not read as «not set».",
					"Уберите пустое значение из %s: оно не читается как «не задано».", k))
		}
		if err := ruleArg(name, items, s); err != nil {
			return nil, err
		}
		canon, err := enumValue(name, items, s)
		if err != nil {
			return nil, err
		}
		out[i] = canon
	}
	return out, nil
}

func ruleArg(k string, p map[string]any, v any) error {
	if n, ok := numberValue(v); ok {
		if min, ok := p["minimum"].(int); ok && n < float64(min) {
			return ruleErr(k, p, i18n.Tf("%s is below the minimum %d", "%s меньше минимума %d", k, min))
		}
		if max, ok := p["maximum"].(int); ok && n > float64(max) {
			return ruleErr(k, p, i18n.Tf("%s is above the maximum %d", "%s больше максимума %d", k, max))
		}
		// An integer of no narrower rule still has the range of the
		// cloud's own: one outside it reached a handler as a number of
		// no meaning (1e30 as a negative int) and went on to the cloud.
		if p["type"] == "integer" && (n < math.MinInt32 || n > math.MaxInt32) {
			return ruleErr(k, p, i18n.Tf("%s is out of range: an integer is between %d and %d",
				"%s вне диапазона: целое число — от %d до %d", k, math.MinInt32, math.MaxInt32))
		}
	}
	// The integers of a list, by the rule of its items.
	if list, isList := v.([]any); isList {
		if items, _ := p["items"].(map[string]any); items["type"] == "integer" {
			for i, it := range list {
				if _, isNumber := numberValue(it); isNumber {
					if err := ruleArg(fmt.Sprintf("%s[%d]", k, i), items, it); err != nil {
						return err
					}
				}
			}
		}
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if min, ok := p["minLength"].(int); ok && len(strings.TrimSpace(s)) < min {
		return ruleErr(k, p, i18n.Tf("%s is empty", "%s пустой", k),
			i18n.Tf("Give %s a value or drop the argument: an empty value is not read as «not set».",
				"Задайте %s значение или уберите аргумент: пустое значение не читается как «не задано».", k))
	}
	if strings.TrimSpace(s) == "" {
		return nil
	}
	switch p["format"] {
	case "uuid":
		if !isUUID(s) {
			return ruleErr(k, p, i18n.Tf("%s: the value is not an id: it is a UUID", "%s: значение — не id: он задаётся UUID", k))
		}
	case "cidr":
		if _, err := netip.ParsePrefix(s); err != nil {
			return ruleErr(k, p, i18n.Tf("%s: the value is not a CIDR (address/prefix length, e.g. 10.0.0.0/16)", "%s: значение — не CIDR (адрес/длина префикса, например 10.0.0.0/16)", k))
		}
	}
	return nil
}

type ruleError struct {
	Arg, Format string
	ce          *clierrors.CLIError
}

func (e *ruleError) Error() string { return e.ce.Error() }
func (e *ruleError) Unwrap() error { return e.ce }

// ruleErr is the refusal of argument k by a rule of its schema p; hint,
// when given, replaces the one that points at the argument's description.
func ruleErr(k string, p map[string]any, msg string, hint ...string) error {
	h := ruleHint(k)
	if len(hint) > 0 {
		h = hint[0]
	}
	re := &ruleError{
		Arg: k,
		ce:  clierrors.New(clierrors.CodeUsage, msg, h),
	}
	if f, ok := p["format"].(string); ok && f != "" {
		re.Format = f
	}
	return re
}

// argRefused reports whether the static rules of tool's schema refuse v
// as its argument arg (ruleArg).
func (r *Registry) argRefused(tool, arg string, v any) bool {
	t, _ := r.Lookup(tool)
	props, _ := t.InputSchema["properties"].(map[string]any)
	p, _ := props[arg].(map[string]any)
	return ruleArg(arg, p, v) != nil
}

var scopeArgs = []string{"project_id", "region", "zone", "availability_zone_id"}

func noEmptyValue(t *Tool, names ...string) {
	props, _ := t.InputSchema["properties"].(map[string]any)
	for _, n := range names {
		if p, ok := props[n].(map[string]any); ok && p["type"] == "string" {
			withMinLength(p)
		}
	}
}

func withMinLength(p map[string]any) map[string]any {
	p["minLength"] = 1
	return p
}

// emptyScopeField is the path of the first scope field (scopeArgs) with an
// empty string value in the object v, a manifest of steps: "" when there
// is none. path names v itself.
func emptyScopeField(v any, path string) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if s, ok := x[k].(string); ok && strings.TrimSpace(s) == "" && slices.Contains(scopeArgs, k) {
				return path + "." + k
			}
			if p := emptyScopeField(x[k], path+"."+k); p != "" {
				return p
			}
		}
	case []any:
		for i, e := range x {
			if p := emptyScopeField(e, path+"["+strconv.Itoa(i)+"]"); p != "" {
				return p
			}
		}
	}
	return ""
}

// ruleHint points at the argument's description, which says where the
// value comes from.
func ruleHint(k string) string {
	return i18n.Tf("See %s in the tool's inputSchema.", "См. %s в inputSchema инструмента.", k)
}

// numberValue is v as a number, when it is one.
func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// scalarOfType reports whether v fits a scalar schema type: boolean,
// integer, number or string. Arrays and objects are not checked: handlers
// take them in more than one form (a comma-separated string for an array,
// a JSON string for k8s_add_addon's config). Numbers come as float64 or
// json.Number from JSON, as Go integers from the stack orchestrator.
func scalarOfType(typ string, v any) bool {
	switch typ {
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "integer", "number":
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case float32:
			f = float64(n)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		case json.Number:
			var err error
			if f, err = n.Float64(); err != nil {
				return false
			}
		default:
			return false
		}
		return typ == "number" || f == math.Trunc(f)
	}
	return true
}

func typeName(typ string) string {
	switch typ {
	case "integer":
		return i18n.T("an integer", "целым числом")
	case "number":
		return i18n.T("a number", "числом")
	case "string":
		return i18n.T("a string", "строкой")
	case "boolean":
		return i18n.T("a boolean (true or false)", "логическим значением (true или false)")
	}
	return "a " + typ
}

func ValueKind(v any) string {
	kind := i18n.T("a number", "число")
	switch v.(type) {
	case string:
		kind = i18n.T("a string", "строка")
	case bool:
		kind = i18n.T("a boolean", "логическое значение")
	case []any:
		kind = i18n.T("an array", "массив")
	case map[string]any:
		kind = i18n.T("an object", "объект")
	}
	return kind
}

var manifestFree = map[string]string{}

// manifestField is a field of a struct of the manifest as JSON names it.
type manifestField struct {
	name string
	typ  reflect.Type
}

// manifestFields are the fields of the struct rt that encoding/json reads:
// by the name of their tag, or of the field.
func manifestFields(rt reflect.Type) []manifestField {
	var out []manifestField
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, manifestField{name, f.Type})
	}
	return out
}

type unknownField struct {
	Path    string `json:"path"`
	Nearest string `json:"nearest,omitempty"`
	level   string
	known   []string
}

var plainField = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// fieldSegment is name k as a path names it.
func fieldSegment(k string) string {
	if plainField.MatchString(k) {
		return k
	}
	if r := []rune(k); len(r) > 64 {
		k = string(r[:64]) + "…"
	}
	return quoteCloud(k)
}

func unknownFields(v any, rt reflect.Type, path string) []unknownField {
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	var out []unknownField
	switch x := v.(type) {
	case map[string]any:
		if rt.Kind() != reflect.Struct || manifestFree[path] != "" {
			return nil
		}
		fields := manifestFields(rt)
		known := make([]string, 0, len(fields))
		for _, f := range fields {
			known = append(known, f.name)
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			i := slices.IndexFunc(fields, func(f manifestField) bool { return strings.EqualFold(f.name, k) })
			if i < 0 {
				out = append(out, unknownField{Path: path + "." + fieldSegment(k), Nearest: nearestName(k, known), level: path, known: known})
				continue
			}
			out = append(out, unknownFields(x[k], fields[i].typ, path+"."+k)...)
		}
	case []any:
		if rt.Kind() != reflect.Slice && rt.Kind() != reflect.Array {
			return nil
		}
		for i, e := range x {
			out = append(out, unknownFields(e, rt.Elem(), path+"["+strconv.Itoa(i)+"]")...)
		}
	}
	return out
}

// nearestName is the name of known nearest to name by its letters, when
// it is within a third of its length: a typo, not another word.
func nearestName(name string, known []string) string {
	best, bestD := "", len(name)/3+1
	for _, k := range known {
		if d := editDistance(strings.ToLower(name), strings.ToLower(k)); d < bestD {
			best, bestD = k, d
		}
	}
	return best
}

// editDistance is the Levenshtein distance of a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
