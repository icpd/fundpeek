package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/icpd/fundsync/internal/model"
)

var ErrNotAuthenticated = errors.New("not authenticated")

type FileStore struct {
	path string
}

type fileData struct {
	Real      *model.RealCredential      `json:"real,omitempty"`
	YangJiBao *model.YangJiBaoCredential `json:"yangjibao,omitempty"`
	XiaoBei   *model.XiaoBeiCredential   `json:"xiaobei,omitempty"`
}

func NewFileStore(path string) (*FileStore, error) {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	return &FileStore{path: path}, nil
}

func (s *FileStore) GetReal() (*model.RealCredential, error) {
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	if data.Real == nil {
		return nil, fmt.Errorf("real: %w", ErrNotAuthenticated)
	}
	return data.Real, nil
}

func (s *FileStore) SaveReal(cred model.RealCredential) error {
	data, err := s.read()
	if err != nil {
		return err
	}
	data.Real = &cred
	return s.write(data)
}

func (s *FileStore) GetYangJiBao() (*model.YangJiBaoCredential, error) {
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	if data.YangJiBao == nil {
		return nil, fmt.Errorf("yangjibao: %w", ErrNotAuthenticated)
	}
	return data.YangJiBao, nil
}

func (s *FileStore) SaveYangJiBao(cred model.YangJiBaoCredential) error {
	data, err := s.read()
	if err != nil {
		return err
	}
	data.YangJiBao = &cred
	return s.write(data)
}

func (s *FileStore) GetXiaoBei() (*model.XiaoBeiCredential, error) {
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	if data.XiaoBei == nil {
		return nil, fmt.Errorf("xiaobei: %w", ErrNotAuthenticated)
	}
	return data.XiaoBei, nil
}

func (s *FileStore) SaveXiaoBei(cred model.XiaoBeiCredential) error {
	data, err := s.read()
	if err != nil {
		return err
	}
	data.XiaoBei = &cred
	return s.write(data)
}

func (s *FileStore) Delete(source string) error {
	data, err := s.read()
	if err != nil {
		return err
	}
	switch source {
	case model.SourceReal:
		data.Real = nil
	case model.SourceYangJiBao:
		data.YangJiBao = nil
	case model.SourceXiaoBei:
		data.XiaoBei = nil
	default:
		return errors.New("unknown credential source")
	}
	return s.write(data)
}

func (s *FileStore) read() (fileData, error) {
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return fileData{}, nil
	}
	if err != nil {
		return fileData{}, err
	}
	var data fileData
	if len(body) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return fileData{}, err
	}
	return data, nil
}

func (s *FileStore) write(data fileData) error {
	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, body, 0o600); err != nil {
		return err
	}
	return os.Chmod(s.path, 0o600)
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}
