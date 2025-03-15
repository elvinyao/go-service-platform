package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func Init() {
	log.Out = os.Stdout
	log.SetLevel(logrus.InfoLevel)
	// 可以根据需要配置日志格式和输出
}

func Infof(format string, args ...interface{}) {
	log.Infof(format, args...)
}

func Errorf(format string, args ...interface{}) {
	log.Errorf(format, args...)
}

func Warnf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// 其他日志方法...
