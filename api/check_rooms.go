//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	data, _ := os.ReadFile(".env")
	var pgURL string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PG_URL=") {
			pgURL = strings.TrimPrefix(line, "PG_URL=")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `SELECT r.id, r.name, r.type FROM rooms r WHERE r.type = 'group' LIMIT 5`)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, rtype string
		rows.Scan(&id, &name, &rtype)
		fmt.Printf("id=%s name=%s type=%s\n", id, name, rtype)
	}

	// Also get a user who is member of a group
	fmt.Println("\nGroup members:")
	mrows, _ := conn.Query(ctx, `SELECT rm.room_id, rm.user_id, u.name FROM room_members rm JOIN users u ON u.id = rm.user_id WHERE rm.room_id IN (SELECT id FROM rooms WHERE type='group') AND rm.status='active' LIMIT 10`)
	defer mrows.Close()
	for mrows.Next() {
		var rid, uid, uname string
		mrows.Scan(&rid, &uid, &uname)
		fmt.Printf("  room=%s user=%s name=%s\n", rid, uid, uname)
	}
}
