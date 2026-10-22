package pgstore

import "gorm.io/gorm"

// numberedQuery counts the filtered set before applying the page offset. A fresh
// GORM session keeps count limits and selected columns out of the rows query.
func numberedQuery[R any](db *gorm.DB, number, limit int) (*gorm.DB, *int64, error) {
	if number == 0 {
		return db, nil, nil
	}
	var total int64
	if err := db.Session(&gorm.Session{}).Model(new(R)).Limit(-1).Offset(-1).Count(&total).Error; err != nil {
		return nil, nil, err
	}
	return db.Offset((number - 1) * limit), &total, nil
}
