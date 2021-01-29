package backend

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/d-tsuji/clipboard"
	"github.com/evercyan/cantor/backend/configs"
	"github.com/evercyan/cantor/backend/internal/git"
	"github.com/evercyan/cantor/backend/tools"
	"github.com/evercyan/letitgo/crypto"
	"github.com/evercyan/letitgo/file"
	"github.com/evercyan/letitgo/util"
	"github.com/sirupsen/logrus"
	"github.com/wailsapp/wails"
)

// App ...
type App struct {
	RT         *wails.Runtime
	Log        *logrus.Logger
	Git        git.Git
	ConfigFile string
	ListFile   string
	List       []map[string]string
}

// WailsInit ...
func (a *App) WailsInit(runtime *wails.Runtime) error {
	a.RT = runtime

	// 日志
	a.Log = tools.NewLogger()
	a.Log.Info("WailsInit")

	// 配置
	configPath := tools.GetConfigPath()
	a.ConfigFile = configPath + "/config.json"
	a.ListFile = configPath + "/database.json"
	a.List = []map[string]string{}

	configContent := file.Read(a.ConfigFile)
	if configContent != "" {
		json.Unmarshal([]byte(configContent), &a.Git)
		// 列表
		listContent := file.Read(a.ListFile)
		if listContent == "" {
			a.List = a.Git.UploadFileList()
			file.Write(a.ListFile, crypto.JsonEncode(a.List))
		} else {
			json.Unmarshal([]byte(listContent), &a.List)
		}
	}

	return nil
}

// WailsShutdown ...
func (a *App) WailsShutdown() {
	a.Log.Info("WailsShutdown")
	return
}

// --------------------------------

func (a *App) updateList(list []map[string]string) error {
	content := crypto.JsonEncode(list)

	// 更新本地文件
	file.Write(a.ListFile, content)

	// 更新仓库文件
	updateListErr := a.Git.Update(configs.GitDBFile, content)
	if updateListErr != nil {
		a.Log.Error("updateListErr: ", updateListErr.Error())
	}

	return updateListErr
}

// --------------------------------

// GetConfig 获取 git 配置和版本信息
func (a *App) GetConfig() *configs.Resp {
	resp := map[string]interface{}{
		"config": a.Git,
		"version": map[string]interface{}{
			"current": configs.Version,
			"last":    a.Git.LastVersion(),
		},
	}
	a.Log.Info("GetConfig resp: ", resp)
	return tools.Success(resp)
}

// SetConfig 更新 git 配置
func (a *App) SetConfig(content string) *configs.Resp {
	a.Log.Info("SetConfig content: ", content)
	if err := json.Unmarshal([]byte(content), &a.Git); err != nil {
		return tools.Fail(err.Error())
	}
	if err := file.Write(a.ConfigFile, content); err != nil {
		return tools.Fail(err.Error())
	}
	return tools.Success("操作成功")
}

// --------------------------------

// GetList 获取文件列表
func (a *App) GetList() *configs.Resp {
	a.Log.Info("GetList count: ", len(a.List))
	return tools.Success(a.List)
}

// --------------------------------

// UploadFile 上传文件
func (a *App) UploadFile() *configs.Resp {
	selectFile := a.RT.Dialog.SelectFile()
	a.Log.Info("UploadFile selectFile: ", selectFile)
	if selectFile == "" {
		return tools.Fail("请选择图片文件")
	}
	if a.Git.Repo == "" {
		return tools.Fail("请设置 Git 配置")
	}

	// 文件格式校验
	fileExt := strings.ToLower(path.Ext(selectFile))
	if !util.InArray(fileExt, configs.AllowFileExts) {
		return tools.Fail("仅支持以下格式: " + strings.Join(configs.AllowFileExts, ", "))
	}

	// 文件大小校验
	fileSize := file.Size(selectFile)
	if fileSize > configs.MaxFileSize {
		return tools.Fail("最大支持 2M 的文件")
	}

	// 文件内容
	fileContent := file.Read(selectFile)
	// 文件路径名称
	fileMd5 := util.Md5(fileContent)
	filePath := fmt.Sprintf(configs.GitFilePath, fileMd5[0:2], fileMd5, fileExt)
	// 请求上传文件
	err := a.Git.Update(filePath, fileContent)
	if err != nil {
		return tools.Fail(err.Error())
	}

	// 更新数据文件
	fileInfo := map[string]string{
		"file_name": path.Base(selectFile),
		"file_md5":  fileMd5,
		"file_size": file.SizeText(fileSize),
		"file_path": filePath,
		"file_url":  a.Git.Url(filePath),
		"create_at": time.Now().Format("2006-01-02 15:04:05"),
	}
	a.Log.Info("UploadFile fileInfo: ", fileInfo)
	a.List = append([]map[string]string{fileInfo}, a.List...)
	go a.updateList(a.List)

	return tools.Success("操作成功")
}

// --------------------------------

// DeleteFile 删除文件
func (a *App) DeleteFile(filePath string) *configs.Resp {
	// 删除文件
	deleteErr := a.Git.Delete(filePath)
	if deleteErr != nil {
		return tools.Fail(deleteErr.Error())
	}

	// 更新数据文件
	list := a.List
	for i := 0; i < len(list); i++ {
		if list[i]["file_path"] == filePath {
			if i == len(list)-1 {
				list = list[:i]
			} else {
				list = append(list[:i], list[i+1:]...)
			}
		}
	}
	a.List = list
	go a.updateList(a.List)

	return tools.Success("操作成功")
}

// CopyFileUrl 复制链接到粘贴板
func (a *App) CopyFileUrl(fileUrl string) *configs.Resp {
	a.Log.Info("CopyFileUrl fileUrl: ", fileUrl)
	err := clipboard.Set(fileUrl)
	if err != nil {
		return tools.Fail(err.Error())
	}
	return tools.Success("已复制到粘贴板")
}

// UpdateFileName 更新文件名称
func (a *App) UpdateFileName(filePath string, fileName string) *configs.Resp {
	a.Log.Infof("UpdateFileName filePath: %v; fileName: %v", filePath, fileName)
	list := a.List
	for i := 0; i < len(list); i++ {
		if list[i]["file_path"] == filePath {
			list[i]["file_name"] = fileName
		}
	}
	a.List = list
	go a.updateList(a.List)

	return tools.Success("操作成功")
}
