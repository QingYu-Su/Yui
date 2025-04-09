package data

import (
	"github.com/glebarez/sqlite" // 导入 SQLite 驱动，用于连接 SQLite 数据库
	"gorm.io/gorm"               // 导入 GORM 包，用于操作数据库
)

var (
	db *gorm.DB // 定义一个全局变量 db，用于存储数据库连接
)

// LoadDatabase 加载并初始化数据库
func LoadDatabase(path string) (err error) {
	// 连接到 SQLite 数据库（可以替换为其他支持的数据库）
	// 参数 path 是数据库文件的路径
	// gorm.Open 用于建立数据库连接，sqlite.Open 是 SQLite 的连接方法
	db, err = gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err // 如果连接失败，返回错误
	}

	// 自动迁移数据库表结构
	// AutoMigrate 会检查数据库中是否存在指定的表：
	// - 如果表不存在，会自动创建表。
	// - 如果表已存在但结构发生变化（如新增字段、修改字段类型等），会自动更新表结构。
	// 注意：AutoMigrate 不会删除表中已有的字段或数据。
	// 这里传入了 Webhook 和 Download 两个结构体，表示需要自动迁移这两个表的结构
	err = db.AutoMigrate(&Webhook{}, &Download{})
	if err != nil {
		return err // 如果自动迁移失败，返回错误
	}

	return nil // 如果一切正常，返回 nil 表示成功
}
