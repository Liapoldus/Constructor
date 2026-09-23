package domain

import "errors"
import "context"

var ErrProjectExists = errors.New("project already exists")

type ProjectRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type ProjectRegistry interface {
	List() ([]ProjectRecord, error)
	Get(id string) (ProjectRecord, error)
	Create(ctx context.Context, id, name string) (ProjectRecord, error)
}
