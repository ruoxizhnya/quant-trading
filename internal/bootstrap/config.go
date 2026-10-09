package bootstrap

import (
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// LoadConfig 读取 YAML 配置。路径来自 CONFIG_PATH env，缺省用
// defaultPath（调用方各传各的服务配置名）。同时按配置设定全局 zerolog 级别。
func LoadConfig(logger zerolog.Logger, defaultPath string) *viper.Viper {
	v := viper.New()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = defaultPath
	}
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	// P0-4: 密钥只允许从 env 注入（YAML 是入库的）。两个名字都认，
	// JWT_SECRET 是历史用法，AUTH_JWT_SECRET 与 AutomaticEnv 的键名一致。
	_ = v.BindEnv("auth.jwt_secret", "JWT_SECRET", "AUTH_JWT_SECRET")
	_ = v.BindEnv("auth.allow_insecure", "AUTH_INSECURE")
	_ = v.BindEnv("auth.insecure_exposure", "AUTH_INSECURE_EXPOSURE")

	if err := v.ReadInConfig(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to read config file")
	}

	logLevel := v.GetString("logging.level")
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	return v
}
