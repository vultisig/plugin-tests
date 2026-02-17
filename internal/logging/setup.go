package logging

import (
	"os"

	"github.com/sirupsen/logrus"
)

func NewLogger(format string) *logrus.Logger {
	if format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyMsg: "_msg",
			},
		})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{
			ForceColors: true,
		})
	}

	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.DebugLevel)

	return logrus.StandardLogger()
}
