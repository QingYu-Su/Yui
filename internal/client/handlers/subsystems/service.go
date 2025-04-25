//go:build windows

package subsystems

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/QingYu-Su/Yui/internal/terminal"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// service 子系统实现Windows服务管理功能
type service bool

// Execute 处理service子系统的命令逻辑
// 参数:
//   - line: 解析后的命令行输入
//   - connection: SSH通道连接
//   - subsystemReq: 子系统请求对象
//
// 返回值:
//   - error: 执行过程中产生的错误
func (s *service) Execute(line terminal.ParsedLine, connection ssh.Channel, subsystemReq *ssh.Request) error {
	subsystemReq.Reply(true, nil) // 确认子系统请求

	// 获取服务名称参数，默认为"rssh"
	name, err := line.GetArgString("name")
	if err == terminal.ErrFlagNotSet {
		name = "rssh"
	}

	// 处理安装服务逻辑
	installPath, err := line.GetArgString("install")
	if err != terminal.ErrFlagNotSet {
		flagErr := err

		// 获取当前可执行文件路径
		currentPath, err := os.Executable()
		if err != nil {
			return errors.New("无法定位当前可执行文件位置: " + err.Error())
		}

		// 如果未指定安装路径，则使用当前路径
		if flagErr != nil {
			installPath = currentPath
		} else if installPath != currentPath {
			// 复制文件到指定安装位置
			input, err := ioutil.ReadFile(currentPath)
			if err != nil {
				return err
			}

			err = ioutil.WriteFile(installPath, input, 0644)
			if err != nil {
				return err
			}
		}

		return s.installService(name, installPath)
	}

	// 处理卸载服务逻辑
	if line.IsSet("uninstall") {
		return s.uninstallService(name)
	}

	// 显示帮助信息
	return errors.New(terminal.MakeHelpText(
		map[string]string{
			"name":      "要操作的服务名称，默认为'rssh'",
			"install":   "可选参数，指定安装路径时会将rssh复制到该位置",
			"uninstall": "卸载由name参数指定的服务",
		},
		"service [模式] [参数|...]",
		"service子系统可以安装或移除rssh二进制文件作为Windows服务",
	))
}

// installService 安装Windows服务
// 参数:
//   - name: 服务名称
//   - location: 服务可执行文件路径
//
// 返回值:
//   - error: 安装过程中产生的错误
func (s *service) installService(name, location string) error {
	// 连接 Windows 服务控制管理器(SCM)
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	// 检查服务是否已存在
	newService, err := m.OpenService(name)
	if err == nil {
		newService.Close()
		return fmt.Errorf("服务 %s 已存在", name)
	}

	// 创建新服务
	newService, err = m.CreateService(
		name,
		location,
		mgr.Config{
			DisplayName: "",
			StartType:   mgr.StartAutomatic, // 设置为自动启动
		},
	)
	if err != nil {
		return err
	}
	defer newService.Close()

	// 配置事件日志
	err = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		newService.Delete()
		return fmt.Errorf("配置事件日志失败: %s", err)
	}

	// 启动服务
	err = newService.Start()
	if err != nil {
		return fmt.Errorf("启动rssh服务失败: %s", err)
	}
	return nil
}

// uninstallService 卸载Windows服务
// 参数:
//   - name: 要卸载的服务名称
//
// 返回值:
//   - error: 卸载过程中产生的错误
func (s *service) uninstallService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	// 打开现有服务
	serviceToRemove, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("服务 %s 未安装", name)
	}
	defer serviceToRemove.Close()

	// 删除服务
	err = serviceToRemove.Delete()
	if err != nil {
		return err
	}

	// 移除事件日志
	eventlog.Remove(name)
	return nil
}
