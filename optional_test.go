package optional

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ----------------------------------------------------------------------------
// 测试用 Options: 复刻 README 中的嵌套配置写法
// ----------------------------------------------------------------------------

type mysqlOptions struct {
	Host              string `mapstructure:"host"`
	MaxOpenConnection int    `mapstructure:"max-open-connection"`
}

func (o *mysqlOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Host, "mysql.host", "127.0.0.1", "MySQL host")
	fs.IntVar(&o.MaxOpenConnection, "mysql.max-open-connection", 200, "Max open connections")
}

type rootOptions struct {
	MySQL *mysqlOptions
}

func newRootOptions() *rootOptions {
	return &rootOptions{MySQL: &mysqlOptions{}}
}

func (o *rootOptions) AddFlags(fs *pflag.FlagSet) {
	o.MySQL.AddFlags(fs)
}

// startService 直接注入命令行参数并启动 Service，返回 Start 的错误。
func startService(s *Service, args ...string) error {
	s.cmd.SetArgs(append([]string{"--silence"}, args...))
	return s.Start(func(Options) error { return nil })
}

// runService 启动一个名为 demo 的 Service 并返回解析后的 Options。
func runService(t *testing.T, args ...string) *rootOptions {
	t.Helper()
	return runServiceNamed(t, "demo", args...)
}

// runServiceNamed 启动指定名称的 Service 并返回解析后的 Options。
func runServiceNamed(t *testing.T, name string, args ...string) *rootOptions {
	t.Helper()

	opts := newRootOptions()
	s := New(name, opts)
	if err := startService(s, args...); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return opts
}

// ----------------------------------------------------------------------------
// 用例
// ----------------------------------------------------------------------------

func TestService_FlagValue(t *testing.T) {
	opts := runService(t, "--mysql.host=10.0.0.5", "--mysql.max-open-connection=10")

	if opts.MySQL.Host != "10.0.0.5" {
		t.Errorf("Host = %q, want %q", opts.MySQL.Host, "10.0.0.5")
	}
	if opts.MySQL.MaxOpenConnection != 10 {
		t.Errorf("MaxOpenConnection = %d, want 10", opts.MySQL.MaxOpenConnection)
	}
}

func TestService_Defaults(t *testing.T) {
	opts := runService(t)

	if opts.MySQL.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", opts.MySQL.Host, "127.0.0.1")
	}
	if opts.MySQL.MaxOpenConnection != 200 {
		t.Errorf("MaxOpenConnection = %d, want 200", opts.MySQL.MaxOpenConnection)
	}
}

// TestService_EnvHyphenatedOption 覆盖带连字符的 flag: 形如
// --mysql.max-open-connection 的选项必须能通过环境变量覆盖。
func TestService_EnvHyphenatedOption(t *testing.T) {
	t.Setenv("DEMO_MYSQL_MAX_OPEN_CONNECTION", "50")
	t.Setenv("DEMO_MYSQL_HOST", "10.0.0.9")

	opts := runService(t)

	if opts.MySQL.MaxOpenConnection != 50 {
		t.Errorf("MaxOpenConnection = %d, want 50 (from env)", opts.MySQL.MaxOpenConnection)
	}
	if opts.MySQL.Host != "10.0.0.9" {
		t.Errorf("Host = %q, want %q (from env)", opts.MySQL.Host, "10.0.0.9")
	}
}

// TestService_ConfigFileHyphenatedOption 覆盖配置文件加载:
// mysql.max-open-connection 必须能写入嵌套结构体字段。
func TestService_ConfigFileHyphenatedOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "mysql:\n  host: 10.0.0.7\n  max-open-connection: 42\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	opts := runService(t, "--config", path)

	if opts.MySQL.MaxOpenConnection != 42 {
		t.Errorf("MaxOpenConnection = %d, want 42 (from config file)", opts.MySQL.MaxOpenConnection)
	}
	if opts.MySQL.Host != "10.0.0.7" {
		t.Errorf("Host = %q, want %q (from config file)", opts.MySQL.Host, "10.0.0.7")
	}
}

// TestService_FlagOverridesEnv 校验优先级: flag > env。
func TestService_FlagOverridesEnv(t *testing.T) {
	t.Setenv("DEMO_MYSQL_MAX_OPEN_CONNECTION", "50")

	opts := runService(t, "--mysql.max-open-connection=7")

	if opts.MySQL.MaxOpenConnection != 7 {
		t.Errorf("MaxOpenConnection = %d, want 7 (flag wins)", opts.MySQL.MaxOpenConnection)
	}
}

// TestService_StartTwice 保证重复 Start 返回错误而不是 panic。
func TestService_StartTwice(t *testing.T) {
	opts := newRootOptions()
	s := New("demo", opts)
	s.cmd.SetArgs([]string{"--silence"})

	if err := s.Start(func(Options) error { return nil }); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if err := s.Start(func(Options) error { return nil }); err != ErrAlreadyStarted {
		t.Errorf("second Start() error = %v, want %v", err, ErrAlreadyStarted)
	}
}

// ----------------------------------------------------------------------------
// 校验行为
// ----------------------------------------------------------------------------

type validatingOptions struct {
	Timeout int
}

func (o *validatingOptions) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&o.Timeout, "timeout", 30, "timeout in seconds")
}

func (o *validatingOptions) Validate() error {
	if o.Timeout <= 0 {
		return errInvalidTimeout
	}
	return nil
}

var errInvalidTimeout = errors.New("timeout must be positive")

func TestService_Validate(t *testing.T) {
	opts := &validatingOptions{}
	s := New("demo", opts)
	s.cmd.SetArgs([]string{"--silence", "--timeout=0"})

	err := s.Start(func(Options) error { return nil })
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !errors.Is(err, errInvalidTimeout) {
		t.Errorf("error = %v, want it to wrap %v", err, errInvalidTimeout)
	}
}

func TestService_SkipCheckOptions(t *testing.T) {
	opts := &validatingOptions{}
	s := New("demo", opts)
	s.cmd.SetArgs([]string{"--silence", "--timeout=0", "--skip-check-options"})

	if err := s.Start(func(Options) error { return nil }); err != nil {
		t.Fatalf("Start() error = %v, want nil when validation is skipped", err)
	}
}

func TestService_RunReceivesOptions(t *testing.T) {
	opts := newRootOptions()
	s := New("demo", opts)
	s.cmd.SetArgs([]string{"--silence"})

	called := false
	if err := s.Start(func(got Options) error {
		called = true
		if got != Options(opts) {
			t.Error("run received unexpected options instance")
		}
		return nil
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !called {
		t.Error("run callback was not called")
	}
}

func TestService_RunErrorPropagates(t *testing.T) {
	opts := newRootOptions()
	s := New("demo", opts)
	s.cmd.SetArgs([]string{"--silence"})

	if err := s.Start(func(Options) error { return errInvalidTimeout }); err == nil {
		t.Fatal("expected run error to propagate, got nil")
	}
}

// ----------------------------------------------------------------------------
// 参数与名称校验
// ----------------------------------------------------------------------------

func TestService_NilOptions(t *testing.T) {
	s := New("demo", nil)
	if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("Start() error = %v, want %v", err, ErrInvalidOptions)
	}
}

func TestService_TypedNilOptions(t *testing.T) {
	var opts *rootOptions
	s := New("demo", opts)
	if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("Start() error = %v, want %v", err, ErrInvalidOptions)
	}
}

type valueOptions struct {
	Addr string
}

func (o valueOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Addr, "addr", ":8080", "listen address")
}

func TestService_NonPointerOptions(t *testing.T) {
	s := New("demo", valueOptions{})
	if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("Start() error = %v, want %v", err, ErrInvalidOptions)
	}
}

type intOptions int

func (o *intOptions) AddFlags(fs *pflag.FlagSet) {}

func TestService_NonStructPointerOptions(t *testing.T) {
	v := intOptions(0)
	s := New("demo", &v)
	if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("Start() error = %v, want %v", err, ErrInvalidOptions)
	}
}

func TestService_EmptyName(t *testing.T) {
	for _, name := range []string{"", "   "} {
		s := New(name, newRootOptions())
		if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrEmptyName) {
			t.Errorf("Start(%q) error = %v, want %v", name, err, ErrEmptyName)
		}
	}
}

func TestService_NilRun(t *testing.T) {
	s := New("demo", newRootOptions())
	if err := s.Start(nil); !errors.Is(err, ErrNilRun) {
		t.Errorf("Start(nil) error = %v, want %v", err, ErrNilRun)
	}
}

type reservedNameOptions struct{}

func (o *reservedNameOptions) AddFlags(fs *pflag.FlagSet) {
	fs.Bool("silence", false, "user-defined silence")
}

func TestService_ReservedFlagName(t *testing.T) {
	s := New("demo", &reservedNameOptions{})
	if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrReservedFlag) {
		t.Errorf("Start() error = %v, want %v", err, ErrReservedFlag)
	}
}

type reservedShorthandOptions struct{}

func (o *reservedShorthandOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolP("verbose", "c", false, "user-defined verbose")
}

func TestService_ReservedFlagShorthand(t *testing.T) {
	s := New("demo", &reservedShorthandOptions{})
	if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrReservedFlag) {
		t.Errorf("Start() error = %v, want %v", err, ErrReservedFlag)
	}
}

type helpShorthandOptions struct{}

func (o *helpShorthandOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolP("verbose", "h", false, "user-defined verbose")
}

func TestService_HelpShorthandReserved(t *testing.T) {
	s := New("demo", &helpShorthandOptions{})
	if err := s.Start(func(Options) error { return nil }); !errors.Is(err, ErrReservedFlag) {
		t.Errorf("Start() error = %v, want %v", err, ErrReservedFlag)
	}
}

// TestService_EnvPrefixSanitized 校验服务名中的非字母数字字符会被转换为下划线。
func TestService_EnvPrefixSanitized(t *testing.T) {
	t.Setenv("MY_SERVICE_MYSQL_HOST", "10.0.0.6")

	opts := runServiceNamed(t, "my-service")

	if opts.MySQL.Host != "10.0.0.6" {
		t.Errorf("Host = %q, want %q (from sanitized env prefix)", opts.MySQL.Host, "10.0.0.6")
	}
}

// ----------------------------------------------------------------------------
// 配置加载
// ----------------------------------------------------------------------------

func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestService_ConfigFileNotFound(t *testing.T) {
	opts := newRootOptions()
	s := New("demo", opts)
	s.cmd.SetArgs([]string{"--silence", "--config", filepath.Join(t.TempDir(), "missing.yaml")})

	err := s.Start(func(Options) error { return nil })
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
	if !strings.Contains(err.Error(), "read option file") {
		t.Errorf("error = %v, want it to mention read option file", err)
	}
}

// TestService_EnvOverridesConfigFile 校验优先级: env > config file。
func TestService_EnvOverridesConfigFile(t *testing.T) {
	path := writeConfig(t, "config.yaml", "mysql:\n  max-open-connection: 42\n")
	t.Setenv("DEMO_MYSQL_MAX_OPEN_CONNECTION", "50")

	opts := runService(t, "-c", path)

	if opts.MySQL.MaxOpenConnection != 50 {
		t.Errorf("MaxOpenConnection = %d, want 50 (env wins over config file)", opts.MySQL.MaxOpenConnection)
	}
}

// TestService_FlagOverridesConfigFile 校验优先级: flag > config file。
func TestService_FlagOverridesConfigFile(t *testing.T) {
	path := writeConfig(t, "config.yaml", "mysql:\n  max-open-connection: 42\n")

	opts := runService(t, "-c", path, "--mysql.max-open-connection=7")

	if opts.MySQL.MaxOpenConnection != 7 {
		t.Errorf("MaxOpenConnection = %d, want 7 (flag wins over config file)", opts.MySQL.MaxOpenConnection)
	}
}

func TestService_JSONConfigFile(t *testing.T) {
	path := writeConfig(t, "config.json", `{"mysql":{"host":"10.0.0.8","max-open-connection":43}}`)

	opts := runService(t, "--config", path)

	if opts.MySQL.Host != "10.0.0.8" {
		t.Errorf("Host = %q, want %q (from JSON config file)", opts.MySQL.Host, "10.0.0.8")
	}
	if opts.MySQL.MaxOpenConnection != 43 {
		t.Errorf("MaxOpenConnection = %d, want 43 (from JSON config file)", opts.MySQL.MaxOpenConnection)
	}
}

func TestService_TOMLConfigFile(t *testing.T) {
	path := writeConfig(t, "config.toml", "[mysql]\nhost = \"10.0.0.9\"\nmax-open-connection = 44\n")

	opts := runService(t, "--config", path)

	if opts.MySQL.Host != "10.0.0.9" {
		t.Errorf("Host = %q, want %q (from TOML config file)", opts.MySQL.Host, "10.0.0.9")
	}
	if opts.MySQL.MaxOpenConnection != 44 {
		t.Errorf("MaxOpenConnection = %d, want 44 (from TOML config file)", opts.MySQL.MaxOpenConnection)
	}
}

// ----------------------------------------------------------------------------
// 子命令与输出
// ----------------------------------------------------------------------------

func TestService_SubcommandInheritsOptions(t *testing.T) {
	t.Setenv("DEMO_MYSQL_HOST", "10.0.0.3")

	opts := newRootOptions()
	s := New("demo", opts)
	subRan := false
	s.AddCommand(&cobra.Command{
		Use: "version",
		Run: func(cmd *cobra.Command, args []string) {
			subRan = true
		},
	})

	if err := startService(s, "version"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !subRan {
		t.Error("subcommand was not executed")
	}
	if opts.MySQL.Host != "10.0.0.3" {
		t.Errorf("Host = %q, want %q (options loaded before subcommand)", opts.MySQL.Host, "10.0.0.3")
	}
}

func TestService_FlagOutput(t *testing.T) {
	opts := newRootOptions()
	s := New("demo", opts)
	var out bytes.Buffer
	s.cmd.SetOut(&out)
	s.cmd.SetArgs([]string{"--mysql.host=10.0.0.5"})

	if err := s.Start(func(Options) error { return nil }); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Flags:") {
		t.Errorf("output = %q, want it to contain %q", got, "Flags:")
	}
	if !strings.Contains(got, "--mysql.host=10.0.0.5") {
		t.Errorf("output = %q, want it to contain the parsed flag value", got)
	}
}

func TestService_SilenceOutput(t *testing.T) {
	opts := newRootOptions()
	s := New("demo", opts)
	var out bytes.Buffer
	s.cmd.SetOut(&out)
	s.cmd.SetArgs([]string{"--silence"})

	if err := s.Start(func(Options) error { return nil }); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want empty with --silence", out.String())
	}
}
