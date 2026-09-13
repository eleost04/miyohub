// Package transfer implements the versioned, passphrase-encrypted account
// archive format. It never reads or writes files and has no network access.
package transfer

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/eleost04/miyohub/internal/model"
)

const (
	MaxFileBytes = 768 << 10
	maxPlaintext = 512 << 10
	iterations   = 600000
	format       = "miyohub-accounts"
)

type Envelope struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Cipher     string `json:"cipher"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func ValidatePassphrase(value string) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 12 || len(value) > 128 {
		return errors.New("迁移密码至少 12 个字符、最多 128 字节")
	}
	return nil
}

func encryption(passphrase string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, passphrase, salt, iterations, 32)
	if err != nil {
		return nil, errors.New("无法生成迁移密钥")
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("无法初始化迁移加密")
	}
	return cipher.NewGCM(block)
}

func Seal(archive model.AccountArchive, passphrase string) (Envelope, error) {
	if err := ValidatePassphrase(passphrase); err != nil {
		return Envelope{}, err
	}
	plain, err := json.Marshal(archive)
	if err != nil {
		return Envelope{}, errors.New("无法编码账号配置")
	}
	defer clear(plain)
	if len(plain) > maxPlaintext {
		return Envelope{}, errors.New("账号配置超过迁移大小限制")
	}
	salt, nonce := make([]byte, 16), make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return Envelope{}, errors.New("无法生成迁移随机盐")
	}
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, errors.New("无法生成迁移随机数")
	}
	aead, err := encryption(passphrase, salt)
	if err != nil {
		return Envelope{}, err
	}
	sealed := aead.Seal(nil, nonce, plain, []byte(format+"/v1/PBKDF2-SHA256/AES-256-GCM"))
	return Envelope{Format: format, Version: 1, KDF: "PBKDF2-SHA256", Iterations: iterations, Cipher: "AES-256-GCM", Salt: base64.StdEncoding.EncodeToString(salt), Nonce: base64.StdEncoding.EncodeToString(nonce), Ciphertext: base64.StdEncoding.EncodeToString(sealed)}, nil
}

func decodeStrict(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return errors.New("迁移文件结构无效或包含不支持的字段")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("迁移文件只允许一个 JSON 对象")
	}
	return nil
}

func Open(raw []byte, passphrase string) (model.AccountArchive, error) {
	if err := ValidatePassphrase(passphrase); err != nil {
		return model.AccountArchive{}, err
	}
	if len(raw) == 0 || len(raw) > MaxFileBytes {
		return model.AccountArchive{}, errors.New("迁移文件为空或超过 768 KiB")
	}
	var envelope Envelope
	if err := decodeStrict(raw, &envelope); err != nil {
		return model.AccountArchive{}, err
	}
	// Never accept arbitrary KDF costs supplied by an unauthenticated file.
	if envelope.Format != format || envelope.Version != 1 || envelope.KDF != "PBKDF2-SHA256" || envelope.Iterations != iterations || envelope.Cipher != "AES-256-GCM" {
		return model.AccountArchive{}, errors.New("不支持的迁移文件版本或加密参数")
	}
	salt, e1 := base64.StdEncoding.Strict().DecodeString(envelope.Salt)
	nonce, e2 := base64.StdEncoding.Strict().DecodeString(envelope.Nonce)
	sealed, e3 := base64.StdEncoding.Strict().DecodeString(envelope.Ciphertext)
	if e1 != nil || e2 != nil || e3 != nil || len(salt) != 16 || len(nonce) != 12 || len(sealed) < 16 || len(sealed) > maxPlaintext+16 {
		return model.AccountArchive{}, errors.New("迁移文件加密字段无效")
	}
	aead, err := encryption(passphrase, salt)
	if err != nil {
		return model.AccountArchive{}, err
	}
	plain, err := aead.Open(nil, nonce, sealed, []byte(format+"/v1/PBKDF2-SHA256/AES-256-GCM"))
	if err != nil {
		return model.AccountArchive{}, errors.New("迁移密码错误或文件已损坏")
	}
	defer clear(plain)
	var archive model.AccountArchive
	if err := decodeStrict(plain, &archive); err != nil {
		return model.AccountArchive{}, err
	}
	if archive.Version != 1 {
		return model.AccountArchive{}, errors.New("不支持的账号配置版本")
	}
	return archive, nil
}
