package ginlog

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Logger *zap.Logger

type logConfig struct {
	Level      string `json:"level"`
	FileName   string `json:"filename"`
	MaxSize    int    `json:"maxsize"`
	MaxAge     int    `json:"max_age"`
	MaxBackups int    `json:"max_backups"`
}

func InitLogger(cfg any) (err error) {
	j, _ := json.Marshal(cfg)
	config := logConfig{}
	err = json.Unmarshal(j, &config)
	if err != nil {
		log.Fatal(err)
	}
	writeSyncer := getLogWriter(&config)
	encoder := getEncoder()
	var l = new(zapcore.Level)
	err = l.UnmarshalText([]byte(config.Level))
	if err != nil {
		log.Fatal(err)
	}

	core := zapcore.NewCore(encoder,
		zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stderr), writeSyncer), l)
	Logger = zap.New(core, zap.AddCaller())

	return
}

func getLogWriter(cfg *logConfig) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   cfg.FileName,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
	}
	return zapcore.AddSync(lumberJackLogger)
}

func getEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	return zapcore.NewJSONEncoder(encoderConfig)
}

func GinLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		path := ctx.Request.URL.Path
		query := ctx.Request.URL.RawQuery
		ctx.Next()

		cost := time.Since(start)
		logger.Info(path,
			zap.Int("status", ctx.Writer.Status()),
			zap.String("method", ctx.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", ctx.ClientIP()),
			zap.String("user-agent", ctx.Request.UserAgent()),
			zap.String("errors", ctx.Errors.ByType(gin.ErrorTypePrivate).String()),
			zap.Duration("cost", cost),
		)
	}
}

// Handle those panic error
func GinRecovery(logger *zap.Logger, stack bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				var brokenPipe bool
				if ne, ok := err.(*net.OpError); ok {
					if se, ok := ne.Err.(*os.SyscallError); ok {
						if strings.Contains(strings.ToLower(se.Error()), "broken pipe") || strings.Contains(strings.ToLower(se.Error()), "connection reset by peer") {
							brokenPipe = true
						}
					}
				}

				httpReuest, _ := httputil.DumpRequest(ctx.Request, false)
				if brokenPipe {
					logger.Error(ctx.Request.URL.Path,
						zap.Any("error", err),
						zap.String("request", string(httpReuest)),
					)
					ctx.Error(err.(error))
					ctx.Abort()
					return
				}

				if stack {
					logger.Error("[Recovery from panic]",
						zap.Any("error", err),
						zap.String("request", string(httpReuest)),
						zap.String("stack", string(debug.Stack())),
					)
				} else {
					logger.Error("[Recover from panic]",
						zap.Any("error", err),
						zap.String("request", string(httpReuest)),
					)
				}
				ctx.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		ctx.Next()
	}
}
