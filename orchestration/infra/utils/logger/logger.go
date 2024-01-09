package logger

import (
	"os"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type stackTracer interface {
	StackTrace() errors.StackTrace
}

func init() {
	logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	level := zerolog.DebugLevel
	logger = logger.Level(level)
	zerolog.DefaultContextLogger = &logger
	zerolog.ErrorStackFieldName = "trace"
	zerolog.ErrorStackMarshaler = func(err error) interface{} {
		if traceErr, ok := err.(stackTracer); ok {
			return traceErr.StackTrace()
		}
		return nil
	}
}

var (
	logger zerolog.Logger
)

func Info(message string) {
	log.Info().Msg(message)
}
func Infof(message string, args ...interface{}) {
	log.Info().Msgf(message, args...)
}
func Debug(message string) {
	log.Debug().Stack().Msg(message)
}
func Debugf(message string, args ...interface{}) {
	log.Debug().Stack().Msgf(message, args...)
}

func ErrorMessage(message string, args ...interface{}) {
	log.Error().Stack().Msgf(message, args...)
}

func Error(err error) {
	log.Error().Stack().Err(err).Send()
}
func Errorf(err error, message string, args ...interface{}) {
	log.Error().Stack().Err(err).Msgf(message, args...)
}

func Fatalf(message string, args ...interface{}) {
	log.Fatal().Caller().Msgf(message, args...)
}
