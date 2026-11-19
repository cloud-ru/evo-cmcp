// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package clock

import "time"

// Now is the time of a call for the values above.
var Now = time.Now

// Since is the time elapsed since t, a reading of Now: the duration an
// answer or a progress line prints.
func Since(t time.Time) time.Duration { return Now().Sub(t) }
