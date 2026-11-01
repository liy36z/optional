package optional

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Options 必须由用户结构体实现，用于注册自定义命令行标志。
//
// 实现类型必须是结构体指针，否则 Start 会返回 ErrInvalidOptions。
type Options interface {
	AddFlags(fs *pflag.FlagSet)
}

// Validatable 可选接口，实现后会在配置加载完成时自动调用 Validate 进行校验。
// 传入 --skip-check-options 可以跳过校验。
type Validatable interface {
	Validate() error
}

var (
	// ErrAlreadyStarted 表示同一个 Service 被 Start 了多次。
	ErrAlreadyStarted = errors.New("optional: service already started")
	// ErrInvalidOptions 表示 New 传入的 opts 为 nil、不是结构体指针或是指向 nil 的指针。
	ErrInvalidOptions = errors.New("optional: options must be a non-nil pointer to a struct")
	// ErrEmptyName 表示 New 传入的服务名为空或只包含空白字符。
	ErrEmptyName = errors.New("optional: service name must not be empty")
	// ErrNilRun 表示 Start 传入的 run 回调为 nil。
	ErrNilRun = errors.New("optional: run function must not be nil")
	// ErrReservedFlag 表示用户注册的标志与框架内置标志重名或简写冲突。
	ErrReservedFlag = errors.New("optional: flag conflicts with built-in flag")
)

// Service 是 CLI 服务框架的核心结构，管理 Cobra 命令、Viper 配置和选项绑定。
type Service struct {
	name string
	opts Options
	cmd  *cobra.Command
	v    *viper.Viper

	started atomic.Bool
}

// New 创建一个 Service 实例。
// name 将作为服务名称，用于命令名称和环境变量前缀。
//
// 环境变量前缀会转为大写，并把非字母、非数字、非下划线的字符替换为下划线。
// 例如 name 为 "my-service" 时，前缀为 MY_SERVICE。
//
// opts 必须是结构体指针。
//
// Example:
//
//	type MyOptions struct {
//	    Addr string
//	}
//
//	func (o *MyOptions) AddFlags(fs *pflag.FlagSet) {
//	    fs.StringVar(&o.Addr, "addr", ":8080", "listen address")
//	}
//
//	s := optional.New("demo", &MyOptions{})
//	err := s.Start(func(opts optional.Options) error {
//	    fmt.Println("Service started")
//	    return nil
//	})
func New(name string, opts Options) *Service {
	return &Service{
		name: name,
		opts: opts,
		cmd:  &cobra.Command{},
		v:    viper.New(),
	}
}

// Start 启动服务，执行给定的 run 函数。
// run 函数会收到已解析和校验的 options。
// 该方法会添加内置标志（--config, --skip-check-options, --silence），
// 解析命令行参数、加载配置文件、绑定环境变量，并自动执行校验（若 opts 实现了 Validatable）。
//
// Start 只能调用一次：重复调用会返回 ErrAlreadyStarted，而不是因为标志重复注册而 panic。
// name 为空、run 为 nil 或 opts 不是非 nil 结构体指针时，返回对应的错误。
//
// Example:
//
//	type MyOptions struct {
//	    Addr string
//	}
//
//	s := optional.New("demo", &MyOptions{})
//	err := s.Start(func(opts optional.Options) error {
//	    o := opts.(*MyOptions)
//	    fmt.Printf("Listening on %s\n", o.Addr)
//	    return nil
//	})
func (s *Service) Start(run func(opts Options) error) error {
	if strings.TrimSpace(s.name) == "" {
		return ErrEmptyName
	}
	if err := checkOptions(s.opts); err != nil {
		return err
	}
	if run == nil {
		return ErrNilRun
	}
	if !s.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	s.cmd.Use = s.name
	s.cmd.Short = fmt.Sprintf("Start %s service", s.name)
	s.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return run(s.opts)
	}

	s.cmd.PersistentFlags().StringP("config", "c", "", "config file (JSON/YAML/TOML)")
	s.cmd.PersistentFlags().Bool("skip-check-options", false, "Skip validation")
	s.cmd.PersistentFlags().BoolP("silence", "s", false, "Suppress flag output")

	if err := s.addOptionsFlags(); err != nil {
		return err
	}

	s.cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return s.initOptions(cmd)
	}

	return s.cmd.Execute()
}

// AddCommand 向服务添加一个子命令。
// 子命令会继承服务注册的所有标志，并在执行前完成配置加载与校验。
// 需要在 Start 之前调用。
//
// Example:
//
//	s := optional.New("demo", &MyOptions{})
//	s.AddCommand(&cobra.Command{
//	    Use:   "version",
//	    Short: "Print version",
//	    Run: func(cmd *cobra.Command, args []string) {
//	        fmt.Println("v1.0.0")
//	    },
//	})
func (s *Service) AddCommand(cmd *cobra.Command) {
	s.cmd.AddCommand(cmd)
}

// checkOptions 校验 opts 是否为非 nil 的结构体指针。
func checkOptions(opts Options) error {
	if opts == nil {
		return ErrInvalidOptions
	}
	rv := reflect.ValueOf(opts)
	if rv.Kind() != reflect.Ptr || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return ErrInvalidOptions
	}
	return nil
}

// addOptionsFlags 在临时 FlagSet 上注册用户标志，检查冲突后再合并到服务，
// 避免与内置标志重名或简写冲突时触发 pflag 的 panic。
func (s *Service) addOptionsFlags() error {
	userFlags := pflag.NewFlagSet(s.name, pflag.ContinueOnError)
	s.opts.AddFlags(userFlags)

	var conflict error
	userFlags.VisitAll(func(f *pflag.Flag) {
		if conflict != nil {
			return
		}
		if s.cmd.PersistentFlags().Lookup(f.Name) != nil {
			conflict = fmt.Errorf("%w: %q", ErrReservedFlag, f.Name)
			return
		}
		if f.Shorthand == "" {
			return
		}
		if f.Shorthand == "h" && f.Name != "help" {
			// cobra 会在执行时自动注册 -h/--help，提前占用会导致 pflag panic。
			conflict = fmt.Errorf("%w: shorthand %q is reserved for --help", ErrReservedFlag, f.Shorthand)
			return
		}
		if existing := s.cmd.PersistentFlags().ShorthandLookup(f.Shorthand); existing != nil {
			conflict = fmt.Errorf("%w: shorthand %q is already used by --%s", ErrReservedFlag, f.Shorthand, existing.Name)
		}
	})
	if conflict != nil {
		return conflict
	}

	s.cmd.PersistentFlags().AddFlagSet(userFlags)
	return nil
}

// envPrefix 把服务名转换为合法的环境变量前缀。
func envPrefix(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		default:
			return '_'
		}
	}, name)
}

// isBuiltinFlag 判断是否为框架内置标志；内置标志不绑定到 viper，避免与用户配置字段冲突。
func isBuiltinFlag(name string) bool {
	switch name {
	case "config", "help", "skip-check-options", "silence":
		return true
	default:
		return false
	}
}

// initOptions 负责配置加载、环境变量绑定、反序列化及校验（内部方法）。
func (s *Service) initOptions(cmd *cobra.Command) error {
	v := s.v

	if optFile, _ := cmd.Flags().GetString("config"); optFile != "" {
		v.SetConfigFile(optFile)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("read option file: %w", err)
		}
	}

	v.SetEnvPrefix(envPrefix(s.name))
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	var bindErr error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if bindErr != nil {
			return
		}
		if isBuiltinFlag(f.Name) {
			return
		}
		// key 必须原样保留 flag 名中的连字符。
		// 若把 "-" 也替换成 "."，形如 mysql.max-open-connection 的 flag 会变成
		// 4 层嵌套 key，导致:
		//   1. viper 的环境变量遮蔽检测 (isPathShadowedInAutoEnv) 会认为
		//      该路径被自身的 env 值遮蔽，从而丢弃环境变量配置；
		//   2. mapstructure 无法把 max.open.connection 映射到字段 MaxOpenConnection。
		// 环境变量名不受影响: 下面的 EnvKeyReplacer 会把 "-" 和 "." 都换成 "_"，
		// 因此 DEMO_MYSQL_MAX_OPEN_CONNECTION 依旧生效。
		key := f.Name
		if err := v.BindPFlag(key, f); err != nil {
			bindErr = fmt.Errorf("bind flag %s: %w", key, err)
			return
		}
	})
	if bindErr != nil {
		return bindErr
	}

	if err := v.Unmarshal(s.opts); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	skipCheck, _ := cmd.Flags().GetBool("skip-check-options")
	if !skipCheck {
		if vv, ok := s.opts.(Validatable); ok {
			if err := vv.Validate(); err != nil {
				return fmt.Errorf("validation: %w", err)
			}
		}
	}

	silence, _ := cmd.Flags().GetBool("silence")
	if !silence {
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "Flags:")
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" {
				fmt.Fprintf(out, "  --%s=%v\n", f.Name, f.Value)
			}
		})
	}

	return nil
}
