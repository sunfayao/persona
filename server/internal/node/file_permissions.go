package node

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// protectedDirs are the folders macOS keeps from programs the user has not allowed (TCC): personad, started over ssh or
// by launchd without Full Disk Access, gets "operation not permitted" there even though the files are the user's own.
var protectedDirs = map[string]string{"Desktop": "桌面", "Documents": "文稿", "Downloads": "下载"}

// explainDenied turns macOS refusing a protected folder into what to do about it; other errors pass through. Who has
// to be given Full Disk Access depends on what started this node (launchedVia).
func explainDenied(path, home string, err error) error {
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	exe, _ := os.Executable()
	via := launchedVia(os.Getenv)
	return explainDeniedFor(path, home, via, fullDiskAccessProgram(via, home, os.Getenv("XPC_SERVICE_NAME"), exe), err)
}

// explainDeniedFor is explainDenied for a node started via ("ssh", "launchd" or ""), exe the program macOS holds it
// to under launchd (fullDiskAccessProgram).
func explainDeniedFor(path, home, via, exe string, err error) error {
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	name := protectedFolderOf(path, home)
	if name == "" {
		return err
	}
	var how string
	switch via {
	case "ssh": // TCC holds what ssh starts to account against sshd-keygen-wrapper, not to personad
		how = "打开 sshd-keygen-wrapper（Persona 经 ssh 在这台 Mac 上运行）"
	case "launchd": // a LaunchAgent's program answers for itself and what it starts (fullDiskAccessProgram)
		if exe == "" {
			exe = "personad"
		}
		how = "点「+」加上 " + exe + " 并打开它（Persona 的 node 由 launchd 服务启动，macOS 按这个服务运行的程序给权限；它更新后又读不了的话，把开关关了再打开）"
	default:
		how = "打开运行 Persona node 的终端或 App"
	}
	return fmt.Errorf("macOS 不让 Persona 读「%s」文件夹里的文件。要读的话，在这台 Mac 的「系统设置 › 隐私与安全性 › 完全磁盘访问权限」里"+
		"%s，或者把文件放到别的文件夹（%w）", name, how, err)
}
