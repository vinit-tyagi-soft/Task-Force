// Command tfadmin runs migrations and loads fictional demo data.
//
//	tfadmin migrate   apply pending migrations
//	tfadmin seed      migrate, then load demo data if the database is empty
//	tfadmin reset     migrate, truncate all data, then load demo data
//
// It connects with TF_MIGRATE_DATABASE_URL (schema owner), never the app role.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/seed"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
	"github.com/vinit-tyagi-soft/taskforce/api/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tfadmin:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: tfadmin migrate|seed|reset")
	}
	url := os.Getenv("TF_MIGRATE_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("TF_MIGRATE_DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}
	switch os.Args[1] {
	case "migrate":
		fmt.Println("migrations applied")
		return nil
	case "seed":
		loaded, err := seed.Load(ctx, pool, false)
		if err != nil {
			return err
		}
		if loaded {
			fmt.Println("demo data loaded")
		} else {
			fmt.Println("database already has data; use reset to reload")
		}
		return nil
	case "reset":
		if _, err := seed.Load(ctx, pool, true); err != nil {
			return err
		}
		fmt.Println("database reset with demo data")
		return nil
	}
	return fmt.Errorf("unknown command %q", os.Args[1])
}
