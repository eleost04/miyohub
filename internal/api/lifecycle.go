package api

func (s *Server) cancelAccountWork(id string) {
	s.records.Invalidate(id)
	s.runner.CancelAccount(id, "账号凭据或所属用户的状态、权限已变更")
	s.exchange.CancelAccount(id)
}
func (s *Server) cancelUserWork(id string) {
	s.archives.cancel(id, "")
	s.probes.cancelUser(id)
	s.qr.Cancel(id)
	s.sms.Cancel(id)
	s.pushBindings.Cancel(id, "")
	s.qqbot.CancelUser(id)
	for _, a := range s.store.AccountsForUser(id, false) {
		s.cancelAccountWork(a.ID)
	}
}
