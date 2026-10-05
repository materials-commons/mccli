package mc

import (
	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
)

type ProjectCreater interface {
	CreateProject(req mcapi.CreateProjectRequest) (*mcmodel.Project, error)
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

type FileDeleter interface {
	DeleteFile(projectID int, fileID int, force bool) error
	DeleteDirectory(projectID int, directoryID int) error
}

type FileMover interface {
	MoveFile(projectID int, fileID int, toDirectoryID int) error
	MoveDirectory(projectID int, dirID int, toDirectoryID int) error
}

type FileRenamer interface {
	RenameFile(projectID int, fileID int, newName string) error
	RenameDirectory(projectID int, dirID int, newName string) error
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

type Loginer interface {
	Login(username, password string) (string, error)
}
