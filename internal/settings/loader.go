package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Document는 저장된 설정 한 벌과 그 버전입니다. 버전 0은 원격 문서 없이 기본값을 쓴 경우입니다.
type Document struct {
	Version int `json:"version"`
	Settings
}

// Source는 원격 설정 문서를 읽습니다.
type Source interface {
	Fetch(ctx context.Context) (Document, error)
}

// Loader는 설정을 원격 → 마지막 정상 값(캐시 파일) → 기본값 순서로 고릅니다.
type Loader struct {
	Source    Source // nil이면 원격을 읽지 않습니다(로컬 개발)
	CachePath string // 마지막으로 정상 적용된 설정을 저장할 파일. 비어 있으면 저장하지 않습니다
}

// Result는 Load의 결과입니다. Origin은 "remote", "cache", "default" 중 하나입니다.
// Problem은 원격이나 캐시를 쓰지 못한 이유입니다. 문제가 없으면 nil입니다.
type Result struct {
	Document
	Origin  string
	Problem error
}

// Load는 적용할 설정을 고릅니다. 원격 값이 정상이면 캐시 파일을 갱신합니다.
func (l Loader) Load(ctx context.Context) Result {
	var problems []error
	if l.Source != nil {
		doc, err := l.Source.Fetch(ctx)
		if err == nil {
			err = doc.Validate()
		}
		if err == nil {
			if werr := l.saveCache(doc); werr != nil {
				problems = append(problems, werr)
			}
			return Result{Document: doc, Origin: "remote", Problem: errors.Join(problems...)}
		}
		problems = append(problems, fmt.Errorf("원격 설정을 쓸 수 없습니다: %w", err))
	}

	doc, err := l.loadCache()
	if err == nil {
		return Result{Document: doc, Origin: "cache", Problem: errors.Join(problems...)}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		problems = append(problems, fmt.Errorf("마지막 정상 설정을 읽을 수 없습니다: %w", err))
	}
	return Result{Document: Document{Settings: Defaults()}, Origin: "default", Problem: errors.Join(problems...)}
}

func (l Loader) loadCache() (Document, error) {
	if l.CachePath == "" {
		return Document{}, fs.ErrNotExist
	}
	data, err := os.ReadFile(l.CachePath)
	if err != nil {
		return Document{}, err
	}
	return Decode(data)
}

// saveCache는 임시 파일에 쓴 뒤 이름을 바꿔서, 쓰는 도중 끊겨도 이전 파일이 깨지지 않게 합니다.
func (l Loader) saveCache(doc Document) error {
	if l.CachePath == "" {
		return nil
	}
	data, err := Encode(doc)
	if err != nil {
		return err
	}
	tmp := l.CachePath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(l.CachePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.CachePath)
}

// Encode는 설정 문서를 JSON으로 바꿉니다. 캐시 파일과 세션 DB에 같은 형식으로 저장합니다.
func Encode(doc Document) ([]byte, error) {
	return json.Marshal(doc)
}

// Decode는 Encode의 결과를 읽고 범위를 다시 검증합니다. 잘못된 값은 에러로 돌려줍니다.
func Decode(data []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return Document{}, fmt.Errorf("설정 JSON 해석 실패: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}
