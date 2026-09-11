package store

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/eleost04/miyohub/internal/model"
)

const passwordIterations = 600000

func hashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		panic(err)
	}
	return "pbkdf2-sha256$" + strconv.Itoa(passwordIterations) + "$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(key)
}
func verifyPassword(password, stored string) bool {
	if len(password) > 128 {
		return false
	}
	parts := strings.Split(stored, "$")
	if len(parts) == 2 { // Legacy hashes migrate after a successful login.
		salt, err := hex.DecodeString(parts[0])
		if err != nil || len(salt) != 16 {
			return false
		}
		actual, err := hex.DecodeString(parts[1])
		if err != nil || len(actual) != 32 {
			return false
		}
		expected := derivePassword(parts[0], password)
		return subtle.ConstantTimeCompare(actual, expected[:]) == 1
	}
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < passwordIterations || iterations > 2000000 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil || len(salt) != 16 {
		return false
	}
	actual, err := hex.DecodeString(parts[3])
	if err != nil || len(actual) != 32 {
		return false
	}
	expected, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	return err == nil && subtle.ConstantTimeCompare(actual, expected) == 1
}
func derivePassword(salt, password string) [32]byte {
	value := []byte(salt + ":" + password)
	var sum [32]byte
	for i := 0; i < 100000; i++ {
		sum = sha256.Sum256(value)
		value = sum[:]
	}
	return sum
}
func dummyVerify(password string) {
	_, _ = pbkdf2.Key(sha256.New, password, []byte("miyohub-no-user!!"), passwordIterations, 32)
}
func (s *Store) ChangePassword(id, oldPassword, newPassword string) (model.User, string, error) {
	if err := validatePassword(newPassword); err != nil {
		return model.User{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.userIndexLocked(id)
	if index < 0 || !verifyPassword(oldPassword, s.data.Users[index].PasswordHash) {
		return model.User{}, "", errors.New("原密码错误")
	}
	s.data.Users[index].PasswordHash = hashPassword(newPassword)
	s.revokeSessionsLocked(id)
	token := s.createSessionLocked(id)
	return publicUser(s.data.Users[index]), token, s.saveLocked()
}
