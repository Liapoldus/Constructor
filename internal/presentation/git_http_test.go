package presentation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
)

type gitBranchHTTPRepository struct {
	created       domain.GitBranch
	checkedOut    domain.GitBranch
	createError   error
	checkoutError error
	createCalls   int
	checkoutCalls int
	lastName      string
}

func (*gitBranchHTTPRepository) Head() (string, error)         { return "head", nil }
func (*gitBranchHTTPRepository) Status() ([]string, error)     { return []string{}, nil }
func (*gitBranchHTTPRepository) Diff() (string, error)         { return "", nil }
func (*gitBranchHTTPRepository) Commit(string) (string, error) { return "head", nil }
func (*gitBranchHTTPRepository) Branches() ([]domain.GitBranch, error) {
	return []domain.GitBranch{}, nil
}
func (*gitBranchHTTPRepository) History(int) ([]domain.GitCommit, error) {
	return []domain.GitCommit{}, nil
}
func (r *gitBranchHTTPRepository) CreateBranch(_ context.Context, name string) (domain.GitBranch, error) {
	r.createCalls++
	r.lastName = name
	return r.created, r.createError
}
func (r *gitBranchHTTPRepository) CheckoutBranch(_ context.Context, name string) (domain.GitBranch, error) {
	r.checkoutCalls++
	r.lastName = name
	return r.checkedOut, r.checkoutError
}

func TestGitBranchEndpointsCreateAndCheckoutWithPermission(t *testing.T) {
	repository := &gitBranchHTTPRepository{
		created:    domain.GitBranch{Name: "feature/editor", Commit: "aabbcc", Current: true},
		checkedOut: domain.GitBranch{Name: "main", Commit: "ddeeff", Current: true},
	}
	handler := NewHandler(nil, nil, nil, nil, nil, nil, application.NewGitService(repository), nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/git/branches", bytes.NewBufferString(`{"name":"feature/editor"}`))
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated || repository.createCalls != 1 || repository.lastName != "feature/editor" {
		t.Fatalf("create response=%d calls=%d name=%q body=%s", createResponse.Code, repository.createCalls, repository.lastName, createResponse.Body.String())
	}
	var created struct {
		Branch domain.GitBranch `json:"branch"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Branch.Name != "feature/editor" || !created.Branch.Current {
		t.Fatalf("unexpected created branch response: %#v", created.Branch)
	}

	checkoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/git/checkout", bytes.NewBufferString(`{"name":"main"}`))
	checkoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(checkoutResponse, checkoutRequest)
	if checkoutResponse.Code != http.StatusOK || repository.checkoutCalls != 1 || repository.lastName != "main" {
		t.Fatalf("checkout response=%d calls=%d name=%q body=%s", checkoutResponse.Code, repository.checkoutCalls, repository.lastName, checkoutResponse.Body.String())
	}
}

func TestGitBranchMutationsRequireCodeProjectPermission(t *testing.T) {
	repository := &gitBranchHTTPRepository{}
	handler := NewHandler(nil, nil, nil, nil, nil, nil, application.NewGitService(repository), nil, nil, nil, application.NewAuthService(deniedPermissionAuthorizer{}), nil)
	for _, path := range []string{"/api/v1/git/branches", "/api/v1/git/checkout"} {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"name":"feature/editor"}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("unauthorized mutation %q returned %d: %s", path, response.Code, response.Body.String())
		}
	}
	if repository.createCalls != 0 || repository.checkoutCalls != 0 {
		t.Fatalf("unauthorized request reached Git repository: create=%d checkout=%d", repository.createCalls, repository.checkoutCalls)
	}
}

func TestGitBranchErrorsUseStableProblemCodes(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code string
		want int
	}{
		{name: "invalid name", err: domain.ErrInvalidGitBranch, code: "git_branch_invalid", want: http.StatusBadRequest},
		{name: "dirty worktree", err: domain.ErrGitWorktreeDirty, code: "git_worktree_dirty", want: http.StatusConflict},
		{name: "duplicate", err: domain.ErrGitBranchExists, code: "git_branch_exists", want: http.StatusConflict},
		{name: "missing", err: domain.ErrGitBranchNotFound, code: "git_branch_not_found", want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &gitBranchHTTPRepository{createError: test.err}
			handler := NewHandler(nil, nil, nil, nil, nil, nil, application.NewGitService(repository), nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/git/branches", bytes.NewBufferString(`{"name":"feature/editor"}`))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			var problem struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if response.Code != test.want || problem.Code != test.code {
				t.Fatalf("status=%d code=%q, want status=%d code=%q body=%s", response.Code, problem.Code, test.want, test.code, response.Body.String())
			}
		})
	}
}
