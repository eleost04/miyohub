package api

func (s *Server) cancelAccountWork(id string) {
	s.runner.CancelAccount(id)
	s.exchange.CancelAccount(id)
}
func (s *Server) cancelUserWork(id string) {
	s.probes.cancelUser(id)
	s.qr.Cancel(id)
	s.sms.Cancel(id)
	s.pushBindings.Cancel(id, "")
	for _, a := range s.store.AccountsForUser(id, false) {
		s.cancelAccountWork(a.ID)
	}
}
