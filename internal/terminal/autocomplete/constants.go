package autocomplete

// 这些是替换标记，例如当 Expect(...) 的输出中出现这些标记时，会查找对应的 map[string]AutoComplete 前缀树
// 然后用于自动补全，这使得自动补全更具上下文感知能力

// RemoteId 是一个内置参数（非用户），用于标识远程 ID
const RemoteId = "<remote_id>"

// Functions 是一个内置参数（非用户），用于标识函数
const Functions = "<functions>"

// WebServerFileIds 是一个内置参数（非用户），用于标识 Web 服务器文件 ID
const WebServerFileIds = "<file_ids>"
