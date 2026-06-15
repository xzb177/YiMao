package services

// UserMappingStore defines the interface for user mapping storage
type UserMappingStore interface {
	GetMoviePilotUserID(telegramID int64) (int64, bool)
	GetMoviePilotUsername(telegramID int64) (string, error)
	GetTelegramUsername(telegramID int64) string
	SetTelegramUsername(telegramID int64, username string)
	AddMapping(telegramID int64, mpUserID int64, mpUsername string) error
	RemoveMapping(telegramID int64) error
	GetTelegramIDByMoviePilotUsername(username string) (int64, bool)
	GetTelegramIDByJellyseerrID(jellyseerrID int64) (int64, bool) // Legacy compat
	GetAllTelegramUsers() []int64
	ForceSave() error
}

// Compile-time check that both implementations satisfy the interface
var _ UserMappingStore = (*UserMappingService)(nil)
var _ UserMappingStore = (*UserMappingDB)(nil)
