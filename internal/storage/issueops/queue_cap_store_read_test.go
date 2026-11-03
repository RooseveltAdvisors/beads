package issueops

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestQueueCapCeilingReadsStoreFirst pins the split-brain fix: the cap a
// create answers to comes from the workspace settings table (what bd config
// set writes), never the viper default when the store has a value. A stored
// 0 turns the cap off; an unset key falls back to viper; a parse failure
// falls back to viper.
func TestQueueCapCeilingReadsStoreFirst(t *testing.T) {
	cases := []struct {
		name     string
		stored   string // empty string = no row
		hasRow   bool
		wantCeil int
	}{
		{"stored 500 beats viper 100", "500", true, 500},
		{"stored 0 turns the cap off", "0", true, 0},
		{"unset falls back to viper default", "", false, 100},
		{"unparseable stored falls back to viper default", "not a number", true, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			if tc.hasRow {
				mock.ExpectQuery("SELECT value FROM config").
					WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(tc.stored))
			} else {
				mock.ExpectQuery("SELECT value FROM config").
					WillReturnRows(sqlmock.NewRows([]string{"value"}))
			}

			if got := queueCapCeilingInTx(context.Background(), tx, QueueCapPerAssigneeKey, 100); got != tc.wantCeil {
				t.Errorf("%s: ceiling = %d, want %d", tc.name, got, tc.wantCeil)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expectations: %v", err)
			}
		})
	}
}
