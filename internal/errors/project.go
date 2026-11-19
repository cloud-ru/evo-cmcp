// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package errors

import "github.com/cloud-ru/evo-cmcp/internal/i18n"

func ProjectRequired() *CLIError {
	return New(CodeConfigInvalid,
		i18n.T("no project is set (project_id)", "не задан проект (project_id)"),
		i18n.T("Set it: 'evo project use <UUID|name>' (kept in the profile), CLOUDRU_PROJECT_ID, or the project_id of the call. The project UUID is in the Cloud.ru console.",
			"Задайте проект: 'evo project use <UUID|имя>' (сохранится в профиле), CLOUDRU_PROJECT_ID или project_id вызова. UUID проекта — в консоли Cloud.ru."))
}
