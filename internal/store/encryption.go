package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const stateFormat = "miyohub-state-v1"

type encryptedState struct {
	Format     string `json:"format"`
	Ciphertext []byte `json:"ciphertext"`
}

func (s *Store) initCipher(required bool) error {
	if s.cipher != nil {
		return nil
	}
	keyPath := s.path + ".key"
	info, err := os.Lstat(keyPath)
	if err == nil && !info.Mode().IsRegular() {
		return errors.New("状态密钥必须是普通文件")
	}
	if errors.Is(err, os.ErrNotExist) {
		if required {
			return errors.New("缺少状态解密密钥，请恢复 state.json.key，禁止覆盖现有状态")
		}
		if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
			return err
		}
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		file, err := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			return s.initCipher(required)
		}
		if err != nil {
			return err
		}
		if _, err = file.Write(key); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else if err != nil {
		return err
	}
	if !s.readOnly {
		if err := os.Chmod(keyPath, 0600); err != nil {
			return err
		}
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	if len(key) != 32 {
		return errors.New("状态密钥长度无效，请恢复原密钥")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	s.cipher, err = cipher.NewGCMWithRandomNonce(block)
	return err
}
func (s *Store) decodeState(raw []byte) ([]byte, error) {
	var envelope encryptedState
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, errors.New("状态文件格式损坏")
	}
	if envelope.Format == "" { // Existing plaintext state is migrated atomically on load.
		if s.readOnly {
			return raw, nil
		}
		if err := s.initCipher(false); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if envelope.Format != stateFormat {
		return nil, errors.New("不支持的状态文件版本")
	}
	if err := s.initCipher(true); err != nil {
		return nil, err
	}
	plain, err := s.cipher.Open(nil, nil, envelope.Ciphertext, []byte(stateFormat))
	if err != nil {
		return nil, errors.New("状态解密失败，文件可能损坏或密钥不匹配")
	}
	return plain, nil
}
func (s *Store) encodeState(raw []byte) ([]byte, error) {
	if err := s.initCipher(false); err != nil {
		return nil, err
	}
	return json.MarshalIndent(encryptedState{Format: stateFormat, Ciphertext: s.cipher.Seal(nil, nil, raw, []byte(stateFormat))}, "", "  ")
}
func atomicStateWrite(path string, raw []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".miyohub-state-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("保存状态失败: %w", err)
	}
	// The file is committed after rename. Directory sync improves crash durability.
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
