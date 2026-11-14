// SPDX-License-Identifier: GPL-3.0-only

package api

// subscription_quiet.go — какие кадры «тихого» обновления (UpdateAllSubscriptions)
// доходят до клиента. Итог по каждой подписке обязан дойти: DONE (обновлена),
// FAILED (сбой) и CONFIRM_REQUIRED (ждёт решения об удалении серверов, группа не
// менялась), иначе клиент не отличит «обновлено» от «пропущено молча». Промежуточные
// кадры не идут вовсе, а REQUESTING несёт адрес подписки, в котором бывает токен.

import lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"

// passesQuiet — кадр этой фазы отправляется, даже когда подробный поток выключен.
func passesQuiet(phase lavrayv1.SubscriptionProgress_Phase) bool {
	switch phase {
	case lavrayv1.SubscriptionProgress_PHASE_FAILED,
		lavrayv1.SubscriptionProgress_PHASE_DONE,
		lavrayv1.SubscriptionProgress_PHASE_CONFIRM_REQUIRED:
		return true
	}
	return false
}
