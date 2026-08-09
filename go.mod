module github.com/materials-commons/mccli

go 1.26.3

require (
	github.com/materials-commons/gomcapi v0.0.7
	github.com/urfave/cli/v3 v3.10.1
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/dmotylev/goproperties v0.0.0-20140630191356-7cbffbaada47 // indirect
	github.com/go-resty/resty/v2 v2.14.0 // indirect
	github.com/gosimple/slug v1.14.0 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/materials-commons/config v0.0.0-20180218183642-ed5747ab2e08 // indirect
	github.com/materials-commons/hydra v1.0.1 // indirect
	github.com/mattn/go-sqlite3 v1.14.49 // indirect
	github.com/spf13/cast v1.7.0 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	gopkg.in/yaml.v1 v1.0.0-20140924161607-9f9df34309c0 // indirect
)

replace github.com/materials-commons/gomcapi => ../gomcapi
