package sqlitejob

type migrationRunner interface {
	RunMigrations() error
}

func (s *Store) RunMigrations() error {
	if m, ok := s.legacy.(migrationRunner); ok {
		return m.RunMigrations()
	}
	return nil
}
