package mihoyo

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

const (
	BBSWebSalt     = "G1ktdwFL4IyGkHuuWSmz0wUe9Db9scyK"
	BBSSalt        = "idMMaGYmVgPzh3wxmWudUXKUPGidO7GM"
	BBSX6Salt      = "t0qEgfub6cvueAPgR5m9aQWWVciEer7v"
	PassportX4Salt = "xV8v4Qu54lUKrEYFZkJhB8cuOh9Asafs"
)

func DS(web bool) string {
	salt := BBSSalt
	if web {
		salt = BBSWebSalt
	}
	t := strconv.FormatInt(time.Now().Unix(), 10)
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	alphabet := "abcdefghijklmnopqrstuvwxyz0123456789"
	r := strings.Builder{}
	for _, b := range buf {
		r.WriteByte(alphabet[int(b)%len(alphabet)])
	}
	return fmt.Sprintf("%s,%s,%s", t, r.String(), md5hex(fmt.Sprintf("salt=%s&t=%s&r=%s", salt, t, r.String())))
}

func DSX6(query, body string) string { return dsWithSalt(BBSX6Salt, query, body, 100001, 200000) }
func DSX4(query, body string) string { return dsWithSalt(PassportX4Salt, query, body, 100000, 200000) }

func dsWithSalt(salt, query, body string, low, high int64) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(high-low+1))
	r := strconv.FormatInt(low+n.Int64(), 10)
	t := strconv.FormatInt(time.Now().Unix(), 10)
	return fmt.Sprintf("%s,%s,%s", t, r, md5hex(fmt.Sprintf("salt=%s&t=%s&r=%s&b=%s&q=%s", salt, t, r, body, query)))
}

func md5hex(value string) string { sum := md5.Sum([]byte(value)); return hex.EncodeToString(sum[:]) }
