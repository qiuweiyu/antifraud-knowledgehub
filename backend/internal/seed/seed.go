package seed

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

func ImportIfEmpty(db *gorm.DB, dataDir string) error {
	var count int64
	db.Model(&database.Category{}).Count(&count)
	if count > 0 {
		return nil
	}
	return Import(db, dataDir)
}

func Import(db *gorm.DB, dataDir string) error {
	if err := importFile(filepath.Join(dataDir, "scam_categories.zh-CN.json"), &[]database.Category{}, func(items []database.Category) error {
		for _, item := range items {
			if err := db.Where("code = ?", item.Code).FirstOrCreate(&item).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := importFile(filepath.Join(dataDir, "risk_rules.zh-CN.json"), &[]database.RiskRule{}, func(items []database.RiskRule) error {
		for _, item := range items {
			if err := db.Where("code = ?", item.Code).FirstOrCreate(&item).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return importFile(filepath.Join(dataDir, "scam_cases_sample.zh-CN.json"), &[]database.ScamCase{}, func(items []database.ScamCase) error {
		for _, item := range items {
			var existing database.ScamCase
			if err := db.Where("title = ?", item.Title).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				if err := db.Create(&item).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func importFile[T any](path string, target *[]T, save func([]T) error) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return save(*target)
}
