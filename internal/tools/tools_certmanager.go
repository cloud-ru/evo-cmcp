// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/cloud-ru/evo-cmcp/internal/certmanager"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

// certClient builds an authenticated Certificate Manager client.
func (s *Registry) certClient(ctx context.Context) (*certmanager.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return certmanager.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func (s *Registry) handleCertList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	certs, err := cli.List(ctx, certmanager.ListOptions{
		Name:       stringArg(args, "name"),
		Type:       stringArg(args, "type"),
		DNSName:    stringArg(args, "dns_name"),
		CommonName: stringArg(args, "common_name"),
		State:      stringArg(args, "state"),
	})
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"project": cli.ProjectID(), "count": len(certs), "certificates": certs}), false
}

func (s *Registry) handleCertGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required", "нужен 'id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	c, err := cli.Get(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(c), false
}

func (s *Registry) planCertIssueLE(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	dnsNames := stringSliceArg(args, "dns_names")
	if name == "" || len(dnsNames) == 0 {
		return answered(usageT("'name' and 'dns_names' (non-empty) are required", "нужны 'name' и непустой 'dns_names'"), true), nil
	}
	preview := map[string]any{
		"message": i18n.Tf("cert_issue_le will request a Let's Encrypt certificate %q for %v (the domains must be delegated to Evolution DNS).",
			"cert_issue_le запросит сертификат Let's Encrypt %q для %v (домены должны быть делегированы в Evolution DNS).", name, dnsNames),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		cli, err := s.certClient(ctx)
		if err != nil {
			return errResult(err), true
		}
		list, taken := certNameTaken(ctx, cli, name)
		if taken != "" {
			return taken, true
		}
		op, err := cli.IssueLetsEncrypt(ctx, name, stringArg(args, "description"), dnsNames)
		if id, _ := op["id"].(string); err != nil || id == "" {
			if err == nil {
				err = noID("operation")
			}
			return lostCreate(ctx, "cert_issue_le", list, i18n.T("certificate", "сертификат"), name, nil, "cert_get id", fmt.Errorf(i18n.T("issue certificate: %w", "выпуск сертификата: %w"), err))
		}
		// Async: return the operation; the caller polls cert_operation until done.
		out := map[string]any{"status": "issuing", "note": i18n.T("Let's Encrypt issuance is async — poll cert_operation with this operation id until done=true. Domains must have delegated zones in Evolution DNS.",
			"Сертификат Let's Encrypt выпускается асинхронно — опрашивайте evo cert operation с id этой операции до done=true. Зоны доменов должны быть делегированы в Evolution DNS."), "operation": op}

		if items, err := list(ctx); err == nil {
			if id, _, ok := takenName(items, name); ok {
				out["certificate_id"] = id
			}
		}
		return toJSON(out), false
	}}, nil
}

func (s *Registry) planCertImport(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	server := pemArg(args, "server_certificate")
	key := pemArg(args, "private_key")
	if name == "" || server == "" || key == "" {
		return answered(usageT("'name', 'server_certificate' and 'private_key' (PEM, or Base64 of it) are required", "нужны 'name', 'server_certificate' и 'private_key' (PEM или он же в Base64)"), true), nil
	}

	preview := map[string]any{
		"name": name,
		"message": i18n.Tf("cert_import will upload certificate %q and its private key to Certificate Manager.",
			"cert_import загрузит сертификат %q и его закрытый ключ в Certificate Manager.", name),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		cli, err := s.certClient(ctx)
		if err != nil {
			return errResult(err), true
		}
		list, taken := certNameTaken(ctx, cli, name)
		if taken != "" {
			return taken, true
		}
		body := map[string]any{"name": name, "serverCertificate": server, "privateKey": key}
		if v := pemArg(args, "intermediate_certificate"); v != "" {
			body["intermediateCertificate"] = v
		}
		if v := stringArg(args, "description"); v != "" {
			body["description"] = v
		}
		c, err := cli.Import(ctx, body)
		if id, _ := c["id"].(string); err != nil || id == "" {
			if err == nil {
				err = noID("certificate")
			}
			return lostCreate(ctx, "cert_import", list, i18n.T("certificate", "сертификат"), name, nil, "cert_get id", fmt.Errorf(i18n.T("import certificate: %w", "импорт сертификата: %w"), err))
		}
		return toJSON(map[string]any{"status": "imported", "certificate": c}), false
	}}, nil
}

// pemArg is a PEM argument of cert_import as Certificate Manager takes it,
// Base64: a PEM given as it is ("-----BEGIN …") is encoded, Base64 goes as
// given (Q17, decision 21.10).
func pemArg(args map[string]any, key string) string {
	v := stringArg(args, key)
	// The blank space before a pasted PEM goes; the PEM itself, its last
	// newline included, is sent as given.
	if pem := strings.TrimLeft(v, " \t\r\n"); strings.HasPrefix(pem, "-----BEGIN ") {
		return base64.StdEncoding.EncodeToString([]byte(pem))
	}
	return v
}

func certNameTaken(ctx context.Context, cli *certmanager.Client, name string) (list func(context.Context) ([]map[string]any, error), taken string) {
	list = func(ctx context.Context) ([]map[string]any, error) {
		return namedResources(cli.List(ctx, certmanager.ListOptions{Name: name}))
	}
	existing, err := list(ctx)
	if err != nil {
		return list, errResult(fmt.Errorf(i18n.T("check the name is free: list certificates: %w", "проверка, что имя свободно: список сертификатов: %w"), err))
	}
	if id, st, ok := takenName(existing, name); ok {
		taken, _ = nameTakenResult(i18n.T("certificate", "сертификат"), name, id, st, "cert_get id="+id)
	}
	return list, taken
}

func (s *Registry) planCertRenew(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "certificate_id")
	if id == "" {
		return answered(usageT("'certificate_id' is required", "нужен 'certificate_id'"), true), nil
	}
	preview := map[string]any{
		"message": i18n.Tf("cert_renew will trigger Let's Encrypt renewal of certificate %q.",
			"cert_renew запустит перевыпуск Let's Encrypt для сертификата %q.", id),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		cli, err := s.certClient(ctx)
		if err != nil {
			return errResult(err), true
		}
		op, err := cli.Renew(ctx, id)
		if err != nil {
			return errResult(err), true
		}
		return toJSON(map[string]any{"status": "renewing", "operation": op}), false
	}}, nil
}

func (s *Registry) planCertDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "id")
	if id == "" {
		return nil, usageErr("'id' is required", "нужен 'id'")
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return nil, err
	}

	if _, err := cli.Get(ctx, id); err != nil {
		return answered(s.deleteFailed("cert_delete", id, err)), nil
	}

	force, _ := args["force"].(bool)
	preview := map[string]any{"force": force, "message": i18n.Tf("cert_delete permanently deletes certificate %q if no service uses it (force=false: if one does, the service refuses the delete, by its specification).",
		"cert_delete безвозвратно удаляет сертификат %q, если его не использует ни один сервис (force=false: если использует, сервис откажет в удалении — по его спецификации).", id),
	}
	if force {
		preview["message"] = i18n.Tf("cert_delete permanently deletes certificate %q even if a service uses it (force=true): a load balancer or listener bound to it loses HTTPS.",
			"cert_delete безвозвратно удаляет сертификат %q, даже если его использует сервис (force=true): привязанный балансировщик или обработчик теряет HTTPS.", id)
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		if err := cli.Delete(ctx, id, force); err != nil {
			return s.deleteFailed("cert_delete", id, err)
		}
		return toJSON(map[string]any{"status": "deleted", "id": id, "force": force}), false
	}}, nil
}

func (s *Registry) handleCertUsage(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required", "нужен 'id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	u, err := cli.Usage(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(u), false
}

func (s *Registry) handleCertVersions(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "certificate_id")
	if id == "" {
		return usageT("'certificate_id' is required", "нужен 'certificate_id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	v, err := cli.Versions(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(v), false
}

func (s *Registry) planCertPrivateKey(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "certificate_id")
	if id == "" {
		return nil, usageErr("'certificate_id' is required", "нужен 'certificate_id'")
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := cli.Get(ctx, id); err != nil {
		return nil, err
	}
	return &Plan{
		Preview: map[string]any{
			"message": i18n.T("cert_private_key returns the SECRET private key in plaintext.",
				"cert_private_key возвращает СЕКРЕТНЫЙ закрытый ключ открытым текстом."),
		},
		Execute: func(ctx context.Context) (string, bool) {
			ver := int64(pgInt(args, "version_id", 0))
			pk, err := cli.PrivateKey(ctx, id, ver)
			if err != nil {
				return errResult(err), true
			}
			pemKey(pk)
			return toJSON(pk), false
		},
	}, nil
}

func pemKey(pk map[string]any) {
	field := "privateKey"
	v, _ := pk[field].(string)
	if v == "" {
		field = "private_key"
		v, _ = pk[field].(string)
	}
	if v == "" {
		return
	}
	if strings.HasPrefix(strings.TrimSpace(v), "-----BEGIN") {
		pk["key_format"] = "pem"
		return
	}
	if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v)); err == nil && strings.HasPrefix(strings.TrimSpace(string(b)), "-----BEGIN") {
		pk[field] = string(b)
		pk["key_format"] = "pem"
		return
	}
	pk["key_format"] = "base64"
}

func (s *Registry) handleCertOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	return s.operation(ctx, "cert_operation", args, func(ctx context.Context) (any, error) { return cli.GetOperation(ctx, id) })
}
