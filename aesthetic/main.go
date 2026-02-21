package aesthetic

import (
	"log"
	"os"
)

var globalAesthetic *AestheticSystem

func InitAesthetic(dataDir, botToken, jellyseerrURL, jellyseerrKey, tmdbKey string) error {
	if globalAesthetic != nil {
		return nil
	}

	dbPath := dataDir + "/bindings.db"

	as, err := NewAestheticSystem(dbPath, botToken, jellyseerrURL, jellyseerrKey, tmdbKey)
	if err != nil {
		return err
	}

	globalAesthetic = as
	log.Printf("[Aesthetic] System initialized at %s", dbPath)

	return nil
}

func GetAesthetic() *AestheticSystem {
	return globalAesthetic
}

func StopAesthetic() {
	if globalAesthetic != nil {
		if globalAesthetic.db != nil {
			globalAesthetic.db.Close()
		}
		log.Println("[Aesthetic] System stopped")
	}
}

func MigrateFromLegacy(legacyDBPath, dataDir string) error {
	adb, err := NewAestheticDB(dataDir + "/bindings.db")
	if err != nil {
		return err
	}
	defer adb.Close()

	_, _ = adb.db.Exec(`
		INSERT OR IGNORE INTO bindings (tg_id, emby_account, realm, points)
		SELECT DISTINCT
			COALESCE(u.telegram_id, 0),
			u.jellyseerr_name,
			CASE WHEN u.total_requests >= 100 THEN 3
			     WHEN u.total_requests >= 50 THEN 2
			     WHEN u.total_requests >= 20 THEN 1
			     ELSE 0
			END,
			u.total_requests
		FROM users u
		WHERE u.jellyseerr_id > 0
	`)

	log.Printf("[Aesthetic] Migrated from legacy database")

	return nil
}

func GetDBForDirectAccess(dataDir string) (*AestheticDB, error) {
	return NewAestheticDB(dataDir + "/bindings.db")
}
