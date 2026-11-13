package localstate

func (s *Store) SavePairing(server string, data []byte) error {
	return s.SaveCredential("pending-pairing:"+server, data)
}
func (s *Store) Pairing(server string) ([]byte, error) {
	return s.Credential("pending-pairing:" + server)
}
func (s *Store) ClearPairing(server string) error {
	return s.ForgetCredential("pending-pairing:" + server)
}
