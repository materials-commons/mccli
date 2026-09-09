package remote

import (
	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
)

type ProjectCreater interface {
	CreateProject(req mcapi.CreateProjectRequest) error
}

type ProjectLister interface {
	ListProjects() ([]mcmodel.Project, error)
}

type ProjectGetter interface {
	GetProject(id int) (*mcmodel.Project, error)
}

type ProjectDeleter interface {
	DeleteProject(id int) error
}

type FileGetter interface {
	GetFile(projectID, fileID int) (*mcmodel.File, error)
	GetFileByPath(projectID int, path string) (*mcmodel.File, error)
}

type DirectoryLister interface {
	ListDirectoryByPath(projectID int, path string) ([]mcmodel.File, error)
}

type FileDirectoryGetter interface {
	FileGetter
	DirectoryLister
}

type DirectoryCreater interface {
	CreateDirectoryByPath(projectID int, path string) error
}
