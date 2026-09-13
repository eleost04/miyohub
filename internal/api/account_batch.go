package api

import (
	"net/http"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func (s *Server) accountBatch(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var input store.AccountBatchPatch
	if !decodeJSON(w, r, &input) {
		return
	}
	accounts, err := s.store.BatchUpdateAccounts(user.ID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	for i, account := range accounts {
		if input.Disabled != nil && *input.Disabled {
			s.cancelAccountWork(account.ID)
		}
		accounts[i] = publicAccount(account)
		accounts[i].ExchangeAllowed = s.store.AccountCanExchange(account.ID)
	}
	s.exchange.Wake()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": accounts})
}
