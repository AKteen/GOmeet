package migrate

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var fsys embed.FS

// Up applies embedded SQL migrations to the Postgres database at dsn.
func Up(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(fsys)
	if err := goose.SetDialect(string(goose.DialectPostgres)); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
