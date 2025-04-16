package commands // 定义包名为commands，包含命令行相关的功能

import (
	"errors"  // 提供错误处理功能
	"fmt"     // 格式化I/O
	"io"      // 基本I/O接口
	"path"    // 处理文件路径
	"regexp"  // 正则表达式支持
	"sort"    // 排序功能
	"strings" // 字符串处理

	// 内部依赖
	"github.com/QingYu-Su/Yui/internal/server/data"           // 数据管理
	"github.com/QingYu-Su/Yui/internal/server/users"          // 用户管理
	"github.com/QingYu-Su/Yui/internal/server/webserver"      // Web服务器功能
	"github.com/QingYu-Su/Yui/internal/terminal"              // 终端交互
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete" // 自动补全
	"github.com/QingYu-Su/Yui/pkg/logger"                     // 日志记录
	"github.com/QingYu-Su/Yui/pkg/table"                      // 表格输出
)

// link结构体定义
type link struct {
}

// 预编译正则表达式，用于匹配一个或多个空白字符
var spaceMatcher = regexp.MustCompile(`[\s]+`)

// ValidArgs方法返回支持的参数及其描述
func (l *link) ValidArgs() map[string]string {
	// 定义参数映射表，键为参数名，值为参数描述
	r := map[string]string{
		"s":                 "Set homeserver address, defaults to server --external_address if set, or server listen address if not",
		"l":                 "List currently active download links",
		"r":                 "Remove download link",
		"C":                 "Comment to add as the public key (acts as the name)",
		"goos":              "Set the target build operating system (default runtime GOOS)",
		"goarch":            "Set the target build architecture (default runtime GOARCH)",
		"goarm":             "Set the go arm variable (not set by default)",
		"name":              "Set the link download url/filename (default random characters)",
		"proxy":             "Set connect proxy address to bake it",
		"tls":               "Use TLS as the underlying transport",
		"ws":                "Use plain http websockets as the underlying transport",
		"wss":               "Use TLS websockets as the underlying transport",
		"stdio":             "Use stdin and stdout as transport, will disable logging, destination after stdio:// is ignored",
		"http":              "Use http polling as the underlying transport",
		"https":             "Use https polling as the underlying transport",
		"use-host-header":   "Use HTTP Host header as callback address when generating download template (add .sh to your download urls and find out)",
		"shared-object":     "Generate shared object file",
		"fingerprint":       "Set RSSH server fingerprint will default to server public key",
		"garble":            "Use garble to obfuscate the binary (requires garble to be installed)",
		"upx":               "Use upx to compress the final binary (requires upx to be installed)",
		"lzma":              "Use lzma compression for smaller binary at the cost of overhead at execution (requires upx flag to be set)",
		"no-lib-c":          "Compile client without glibc",
		"sni":               "When TLS is in use, set a custom SNI for the client to connect with",
		"working-directory": "Set download/working directory for automatic script (i.e doing curl https://<url>.sh)",
		"raw-download":      "Download over raw TCP, outputs bash downloader rather than http",
		"use-kerberos":      "Instruct client to try and use kerberos ticket when using a proxy",
		"log-level":         "Set default output logging levels, [INFO,WARNING,ERROR,FATAL,DISABLED]",
		"ntlm-proxy-creds":  "Set NTLM proxy credentials in format DOMAIN\\USER:PASS",
	}

	// 定义参数映射表，键为参数名，值为参数描述，由于owners和o的描述相同，故使用该函数进行添加
	addDuplicateFlags("Set owners of client, if unset client is public all users. E.g --owners jsmith,ldavidson", r, "owners", "o")

	return r // 返回完整的参数映射表
}

// Run 方法是 link 结构体的主要执行方法，处理用户命令
func (l *link) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 处理 -l/--list 标志：列出当前活动的下载链接
	if toList, ok := line.Flags["l"]; ok {
		// 创建表格用于显示结果
		t, _ := table.NewTable("Active Files", "Url", "Client Callback", "Log Level", "GOOS", "GOARCH", "Version", "Type", "Hits", "Size")

		// 获取下载文件列表
		files, err := data.ListDownloads(strings.Join(toList.ArgValues(), " "))
		if err != nil {
			return err
		}

		// 对文件ID进行排序
		ids := []string{}
		for id := range files {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		// 将文件信息添加到表格中
		for _, id := range ids {
			file := files[id]
			t.AddValues(
				"http://"+path.Join(webserver.DefaultConnectBack, id), // 完整URL
				file.CallbackAddress,                  // 回调地址
				file.LogLevel,                         // 日志级别
				file.Goos,                             // 目标操作系统
				file.Goarch+file.Goarm,                // 目标架构
				file.Version,                          // 版本号
				file.FileType,                         // 文件类型
				fmt.Sprintf("%d", file.Hits),          // 访问次数
				fmt.Sprintf("%.2f MB", file.FileSize), // 文件大小
			)
		}

		// 输出表格到终端
		t.Fprint(tty)
		return nil
	}

	// 处理 -r/--remove 标志：删除下载链接
	if toRemove, ok := line.Flags["r"]; ok {
		// 检查是否提供了要删除的链接参数
		if len(toRemove.Args) == 0 {
			fmt.Fprintf(tty, "No argument supplied\n")
			return nil
		}

		// 获取匹配的下载文件
		files, err := data.ListDownloads(strings.Join(toRemove.ArgValues(), " "))
		if err != nil {
			return err
		}

		// 检查是否有匹配的文件
		if len(files) == 0 {
			return errors.New("No links match")
		}

		// 逐个删除文件
		for id := range files {
			err := data.DeleteDownload(id)
			if err != nil {
				fmt.Fprintf(tty, "Unable to remove %s: %s\n", id, err)
				continue
			}
			fmt.Fprintf(tty, "Removed %s\n", id)
		}

		return nil
	}

	// 以下是创建新下载链接的逻辑

	// 初始化构建配置
	buildConfig := webserver.BuildConfig{
		SharedLibrary:   line.IsSet("shared-object"), // 是否生成共享库
		UPX:             line.IsSet("upx"),           // 是否使用UPX压缩
		Lzma:            line.IsSet("lzma"),          // 是否使用LZMA压缩
		Garble:          line.IsSet("garble"),        // 是否使用代码混淆
		DisableLibC:     line.IsSet("no-lib-c"),      // 是否禁用glibc
		UseKerberosAuth: line.IsSet("use-kerberos"),  // 是否使用Kerberos认证
		RawDownload:     line.IsSet("raw-download"),  // 是否使用原始TCP下载
	}

	// 获取并设置各种构建参数
	var err error
	buildConfig.GOOS, err = line.GetArgString("goos") // 目标操作系统
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	buildConfig.GOARCH, err = line.GetArgString("goarch") // 目标架构
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	buildConfig.GOARM, err = line.GetArgString("goarm") // ARM版本
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	// 设置连接回地址
	buildConfig.ConnectBackAdress, err = line.GetArgString("s")
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}
	if buildConfig.ConnectBackAdress == "" {
		buildConfig.ConnectBackAdress = webserver.DefaultConnectBack
	}

	// 是否使用Host头
	buildConfig.UseHostHeader = line.IsSet("use-host-header")

	// 检查传输协议设置（只能选择一种）
	tt := map[string]bool{
		"tls":   line.IsSet("tls"),   // TLS传输
		"wss":   line.IsSet("wss"),   // WebSocket安全传输
		"ws":    line.IsSet("ws"),    // WebSocket传输
		"stdio": line.IsSet("stdio"), // 标准输入输出
		"http":  line.IsSet("http"),  // HTTP轮询
		"https": line.IsSet("https"), // HTTPS轮询
	}

	// 确保只选择了一种传输协议
	numberTrue := 0
	scheme := ""
	for i := range tt {
		if tt[i] {
			numberTrue++
			scheme = i + "://"
		}
	}
	if numberTrue > 1 {
		return errors.New("cant use tls/wss/ws/std/http/https flags together (only supports one per client)")
	}

	// 设置完整的连接回地址（包含协议）
	buildConfig.ConnectBackAdress = scheme + buildConfig.ConnectBackAdress

	// 获取更多配置参数
	buildConfig.Name, err = line.GetArgString("name") // 文件名
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	buildConfig.Comment, err = line.GetArgString("C") // 注释/名称
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	buildConfig.Fingerprint, err = line.GetArgString("fingerprint") // 服务器指纹
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	buildConfig.Proxy, err = line.GetArgString("proxy") // 代理地址
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	buildConfig.SNI, err = line.GetArgString("sni") // SNI设置
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	// 设置日志级别
	buildConfig.LogLevel, err = line.GetArgString("log-level")
	if err != nil {
		if err != terminal.ErrFlagNotSet {
			return err
		}
		// 默认使用当前日志级别
		buildConfig.LogLevel = logger.UrgencyToStr(logger.GetLogLevel())
	} else {
		// 验证日志级别是否有效
		_, err := logger.StrToUrgency(buildConfig.LogLevel)
		if err != nil {
			return fmt.Errorf("could to turn log-level %q into log urgency (probably an invalid setting)", err)
		}
	}

	// 设置所有者（支持owners或o两种参数名）
	buildConfig.Owners, err = line.GetArgString("owners")
	if err != nil {
		if err != terminal.ErrFlagNotSet {
			return err
		}
		buildConfig.Owners, err = line.GetArgString("o")
		if err != nil && err != terminal.ErrFlagNotSet {
			return err
		}
	}

	// 检查所有者参数是否包含空格
	if spaceMatcher.MatchString(buildConfig.Owners) {
		return errors.New("owners flag cannot contain any whitespace")
	}

	// 获取更多可选参数
	buildConfig.WorkingDirectory, err = line.GetArgString("working-directory") // 工作目录
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	buildConfig.NTLMProxyCreds, err = line.GetArgString("ntlm-proxy-creds") // NTLM代理凭据
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	// 构建下载链接
	url, err := webserver.Build(buildConfig)
	if err != nil {
		return err
	}

	// 输出生成的URL到终端
	fmt.Fprintln(tty, url)

	return nil
}

// Expect 方法用于实现命令的自动补全功能
func (l *link) Expect(line terminal.ParsedLine) []string {
	// 检查是否有命令片段（如子命令）
	if line.Section != nil {
		// 根据子命令返回不同的自动补全建议
		switch line.Section.Value() {
		case "l", "r": // 如果是list或remove子命令
			// 返回Web服务器文件ID列表用于自动补全
			return []string{autocomplete.WebServerFileIds}
		}
	}

	// 默认情况下不提供自动补全建议
	return nil
}

// Help 方法提供命令的帮助信息
func (e *link) Help(explain bool) string {
	// 如果只需要简短说明
	if explain {
		return "Generate client binary and return link to it"
	}

	// 返回完整的帮助文本，包括参数说明和使用示例
	return terminal.MakeHelpText(
		e.ValidArgs(),    // 获取命令的有效参数列表
		"link [OPTIONS]", // 命令使用格式
		"Link will compile a client and serve the resulting binary on a link which is returned.", // 详细描述
		"This requires the web server component has been enabled.",                               // 额外说明
	)
}
