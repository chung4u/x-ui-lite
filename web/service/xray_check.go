package service

// CheckConfiguration validates the database's effective configuration without
// binding listeners, replacing files, or interrupting the running core.
func (s *XrayService) CheckConfiguration() error {
	config, err := s.GetXrayConfig()
	if err != nil {
		return err
	}
	return validateXrayRuntimeConfig(config)
}
