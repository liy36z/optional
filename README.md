# optional

基于 [cobra](https://github.com/spf13/cobra) + [viper](https://github.com/spf13/viper) + [pflag](https://github.com/spf13/pflag) 的 Go CLI 服务启动框架。

一次调用即可完成：命令行标志注册与解析、配置文件加载、环境变量绑定、参数校验、子命令挂载。

## 特性

- 标志、环境变量、配置文件多来源配置，优先级明确
- 支持嵌套配置（`--mysql.host` 对应 `mysql.host`）
- 可选 `Validate()` 自动校验，支持 `--skip-check-options` 跳过
- 支持挂载 cobra 子命令，子命令自动继承标志与配置加载
- 内置参数冲突检查，误用保留名称时返回错误而不是 panic
- 重复 `Start` 返回 `ErrAlreadyStarted`，不会重复注册标志导致 panic

## 环境要求

- Go 1.25 及以上

## 安装

```bash
go get github.com/liy36z/optional
```

## 快速开始

### 1. 定义 Options

```go
package main

import (
	"fmt"

	"github.com/liy36z/optional"
	"github.com/spf13/pflag"
)

type MyOptions struct {
	Addr    string `mapstructure:"addr"`
	Timeout int    `mapstructure:"timeout"`
	Debug   bool   `mapstructure:"debug"`
}

func (o *MyOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Addr, "addr", ":8080", "HTTP server listen address")
	fs.IntVar(&o.Timeout, "timeout", 30, "Request timeout in seconds")
	fs.BoolVar(&o.Debug, "debug", false, "Enable debug mode")
}
```

`Options` 必须传入**非 nil 的结构体指针**，否则 `Start` 返回 `ErrInvalidOptions`。
`AddFlags` 必须使用指针接收者，`viper.Unmarshal` 才能把配置写回结构体。

### 2. （可选）实现参数校验

```go
func (o *MyOptions) Validate() error {
	if o.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}
```

实现 `Validatable` 后，配置加载完成时会自动调用 `Validate`。
校验失败时 `Start` 返回包装了原始错误的 error，可使用 `errors.Is` 判断。
传入 `--skip-check-options` 可跳过校验。

### 3. 启动服务

```go
func main() {
	opts := &MyOptions{}
	s := optional.New("demo", opts)

	if err := s.Start(func(o optional.Options) error {
		cfg := o.(*MyOptions)
		fmt.Printf("listening on %s (timeout=%d, debug=%v)\n", cfg.Addr, cfg.Timeout, cfg.Debug)
		return nil
	}); err != nil {
		// Start 返回的错误包括参数解析、配置加载、校验和 run 回调返回的错误
		panic(err)
	}
}
```

运行方式：

```bash
go run . --addr=:9090 --debug          # 命令行标志
DEMO_ADDR=:9090 go run .               # 环境变量
go run . -c config.yaml                # 配置文件
go run . --silence --addr=:9090        # 不打印标志值
```

## 内置标志

| 标志 | 简写 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `""` | 配置文件路径；指定后文件必须存在 |
| `--skip-check-options` | - | `false` | 跳过 `Validate()` 校验 |
| `--silence` | `-s` | `false` | 不打印最终的标志值 |
| `--help` | `-h` | `false` | cobra 自动添加的帮助标志 |

内置标志不会作为标志键绑定到 viper，因此命令行中的内置标志值不会被反序列化到 `Options` 中。这也意味着：

- 配置文件路径只能通过 `-c/--config` 指定，环境变量 `DEMO_CONFIG` 不会生效
- `--silence`、`--skip-check-options` 也不受环境变量或配置文件控制

**安全提示**：默认行为会把所有标志名和解析后的值打印到标准输出，其中可能包含密码等敏感信息。
生产环境或日志会采集 stdout 的场景请始终传入 `--silence`。

## 配置来源与优先级

从高到低：

1. 命令行中显式设置的标志（如 `--addr=:9090`）
2. 环境变量（如 `DEMO_ADDR`）
3. 配置文件（通过 `-c/--config` 指定）
4. `AddFlags` 中注册的标志默认值

配置文件格式由扩展名决定，支持 JSON、YAML、TOML 等 viper 支持的格式：

```yaml
# config.yaml
addr: ":9090"
timeout: 60
debug: true
```

```json
{
  "addr": ":9090",
  "timeout": 60,
  "debug": true
}
```

## 环境变量规则

环境变量名由「服务名前缀 + 标志名」构成：

1. 服务名转为大写，非字母、数字、下划线的字符替换为 `_`
2. 标志名中的 `.` 和 `-` 都替换为 `_`

| 服务名 | 标志 | 环境变量 |
|--------|------|----------|
| `demo` | `--addr` | `DEMO_ADDR` |
| `demo` | `--mysql.host` | `DEMO_MYSQL_HOST` |
| `demo` | `--mysql.max-open-connection` | `DEMO_MYSQL_MAX_OPEN_CONNECTION` |
| `my-service` | `--mysql.host` | `MY_SERVICE_MYSQL_HOST` |

环境变量只能覆盖 viper 已知的键：已注册的标志，或配置文件中出现过的键。既没有注册为标志、也没有出现在配置文件中的字段，无法仅通过环境变量注入。

## 嵌套配置

标志名中的 `.` 表示配置层级：`--mysql.host` 对应配置文件中的 `mysql.host`。

```go
type MySQLOptions struct {
	Host              string `mapstructure:"host"`
	MaxOpenConnection int    `mapstructure:"max-open-connection"`
}

func (o *MySQLOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Host, "mysql.host", "127.0.0.1", "MySQL host")
	fs.IntVar(&o.MaxOpenConnection, "mysql.max-open-connection", 200, "Max open connections")
}

type MyOptions struct {
	MySQL *MySQLOptions `mapstructure:"mysql"`
}

func (o *MyOptions) AddFlags(fs *pflag.FlagSet) {
	o.MySQL.AddFlags(fs)
}
```

```yaml
# config.yaml
mysql:
  host: 10.0.0.5
  max-open-connection: 100
```

```bash
./app --mysql.host=10.0.0.5 --mysql.max-open-connection=50
DEMO_MYSQL_MAX_OPEN_CONNECTION=50 ./app -c config.yaml
```

### mapstructure 标签注意事项

当标志名包含 `-` 或配置键名与 Go 字段名不一致时，必须显式声明 `mapstructure` 标签：

```go
MaxOpenConnection int `mapstructure:"max-open-connection"`
```

否则 mapstructure 无法把 `max-open-connection` 匹配到 `MaxOpenConnection`，配置文件和环境变量会被**静默忽略**。

## 子命令

```go
func main() {
	opts := &MyOptions{}
	s := optional.New("demo", opts)

	s.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("v1.0.0")
		},
	})

	if err := s.Start(func(o optional.Options) error {
		// 仅当执行根命令时调用
		return nil
	}); err != nil {
		panic(err)
	}
}
```

```bash
./app version
./app --config config.yaml version
```

说明：

- `AddCommand` 必须在 `Start` 之前调用
- 子命令继承服务注册的所有标志
- 执行子命令前同样会加载配置、绑定环境变量并执行校验
- 根命令的 `run` 回调不会因子命令执行而触发
- 如果子命令自定义了 `PersistentPreRunE`，会覆盖框架的预执行逻辑（cobra 语义），此时配置不会自动加载
- 子命令自身注册的标志也会参与配置绑定与反序列化，请避免与根 `Options` 的字段名重名

## 完整示例：自定义标志 + 校验 + 子命令

```go
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/liy36z/optional"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Options struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	Verbose  bool   `mapstructure:"verbose"`
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Addr, "addr", ":8080", "listen address")
	fs.StringVar(&o.Password, "password", "", "service password")
	fs.BoolVar(&o.Verbose, "verbose", false, "enable verbose logging")
}

func (o *Options) Validate() error {
	if o.Addr == "" {
		return errors.New("addr must not be empty")
	}
	if o.Password == "" {
		return errors.New("password must not be empty")
	}
	return nil
}

func main() {
	opts := &Options{}
	s := optional.New("demo", opts)

	s.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("v1.0.0")
		},
	})

	if err := s.Start(func(o optional.Options) error {
		cfg := o.(*Options)
		fmt.Printf("listening on %s, verbose=%v\n", cfg.Addr, cfg.Verbose)
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}
```

```bash
go run . --password=secret --addr=:9090
DEMO_PASSWORD=secret go run .
go run . --password=secret -c config.yaml
go run . --password=secret -s          # 避免把密码打印到 stdout
```

## API

### `func New(name string, opts Options) *Service`

创建 `Service` 实例。

- `name`：服务名，用作 cobra 命令名和环境变量前缀
- `opts`：必须是非 nil 的结构体指针
- 服务名不能为空或只包含空白字符（`Start` 时检查）

### `func (s *Service) Start(run func(opts Options) error) error`

注册内置标志和用户标志，解析 `os.Args`，加载配置，执行校验，然后调用 `run`。

- `run` 收到的是 `New` 传入的同一个 `Options` 实例
- `run` 返回的 error 会作为 `Start` 的返回值返回
- 同一个 `Service` 只能成功启动一次；通过前置参数校验后再次调用返回 `ErrAlreadyStarted`
- 参数校验失败时返回包装了原始错误的 error

### `func (s *Service) AddCommand(cmd *cobra.Command)`

挂载子命令，必须在 `Start` 之前调用。

### `type Options interface { AddFlags(fs *pflag.FlagSet) }`

必须实现。用于注册自定义命令行标志。

### `type Validatable interface { Validate() error }`

可选实现。配置加载完成后自动调用；`--skip-check-options` 可跳过。

## 错误

| 错误 | 触发条件 |
|------|----------|
| `ErrAlreadyStarted` | 通过前置参数校验后，同一个 `Service` 再次调用 `Start` |
| `ErrInvalidOptions` | `opts` 为 nil、不是结构体指针或是指向 nil 的指针 |
| `ErrEmptyName` | 服务名为空或只包含空白字符 |
| `ErrNilRun` | `Start` 的 `run` 回调为 nil |
| `ErrReservedFlag` | 用户标志与内置标志重名，或简写（`-c`、`-s`、`-h`）冲突 |

以上错误均为导出的哨兵错误，可使用 `errors.Is` 判断：

```go
if err := s.Start(run); errors.Is(err, optional.ErrAlreadyStarted) {
	// ...
}
```

## 注意事项

- `opts` 必须是结构体指针；`AddFlags` 必须使用指针接收者
- 内置标志名称 `config`、`skip-check-options`、`silence` 及简写 `c`、`s` 为保留名称；`-h` 保留给 `--help`
- `Start` 只能成功调用一次
- 指定配置文件后，文件必须存在且能被 viper 解析，否则 `Start` 返回错误
- 默认会打印标志值，可能泄露敏感信息，建议生产环境使用 `--silence`
- 含 `-` 的标志名需要 `mapstructure` 标签，否则配置会静默丢失
- 环境变量只对 viper 已知的键生效，无法注入未注册且未出现在配置文件中的字段

## 开发与测试

```bash
gofmt -l .
go vet ./...
go test -race ./...
```
