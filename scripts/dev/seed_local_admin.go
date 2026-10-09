// Herramienta EXCLUSIVA para una demostración local en 127.0.0.1.
// Establece usuario admin / contraseña admin en data/secure-demo.sqlite.
// NO usar estas credenciales en un servidor accesible por Internet.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	dbFile := filepath.Join(root, "data", "secure-demo.sqlite")
	if err = os.MkdirAll(filepath.Dir(dbFile), 0700); err != nil {
		panic(err)
	}
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS goradio_users (
  id TEXT PRIMARY KEY,username TEXT UNIQUE COLLATE NOCASE NOT NULL,
  password_hash TEXT NOT NULL,role TEXT NOT NULL,created_at INTEGER NOT NULL
 )`)
	if err != nil {
		panic(err)
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		panic(err)
	}
	hash := argon2.IDKey([]byte("admin"), salt, 3, 64*1024, 2, 32)
	encoded := fmt.Sprintf("argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		panic(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO goradio_users(id,username,password_hash,role,created_at)
 VALUES(?, 'admin', ?, 'admin', ?)
 ON CONFLICT(username) DO UPDATE SET password_hash=excluded.password_hash,role='admin'`,
		hex.EncodeToString(id), encoded, time.Now().Unix())
	if err != nil {
		panic(err)
	}
	fmt.Println("Cuenta local de demostración: admin / admin")
	fmt.Println("Base de datos:", dbFile)
	fmt.Println("ADVERTENCIA: no compartir este servidor a Internet con estas credenciales.")
}
