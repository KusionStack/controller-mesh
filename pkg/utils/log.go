/*
Copyright 2023 The KusionStack Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	pid      = os.Getpid()
	program  = filepath.Base(os.Args[0])
	host     = "unknownhost"
	userName = "unknownuser"

	logDir   = "/home/admin/logs"
	logFile  = "kridge.log"
	logLevel = "info"
	// 100Mi
	maxSize = 200
	// max 5 backups
	maxBackups = 10
	// max 28 days ages of backups
	maxAge = 7

	levelMap = map[string]zapcore.LevelEnabler{
		"info":  zapcore.InfoLevel,
		"INFO":  zapcore.InfoLevel,
		"debug": zapcore.DebugLevel,
		"DEBUG": zapcore.DebugLevel,
	}
)

// Copy from klog
// shortHostname returns its argument, truncating at the first period.
// For instance, given "www.google.com" it returns "www".
func shortHostname(hostname string) string {
	if i := strings.Index(hostname, "."); i >= 0 {
		return hostname[:i]
	}
	return hostname
}

func init() {
	h, err := os.Hostname()
	if err == nil {
		host = shortHostname(h)
	}

	current, err := user.Current()
	if err == nil {
		userName = current.Username
	}

	// Sanitize userName since it may contain filepath separators on Windows.
	userName = strings.Replace(userName, `\`, "_", -1)
}

func validate() error {
	_, ok := levelMap[logLevel]
	if !ok {
		return fmt.Errorf("-zap-log-info %s is not supported, please use info or debug", logLevel)
	}

	if maxSize < 0 {
		return fmt.Errorf("-zap-max-size value should greater than 0, got %d", maxSize)
	}

	if maxBackups < 0 {
		return fmt.Errorf("-zap-max-backups value should greater than 0, got %d", maxBackups)
	}

	if maxAge < 0 {
		return fmt.Errorf("-zap-max-age value should greater than 0, got %d", maxAge)
	}

	return nil
}

func InitFlags(flagset *flag.FlagSet) {
	if flagset == nil {
		flagset = flag.CommandLine
	}

	flagset.StringVar(&logDir, "zap-log-dir", logDir, "Directory of the file to write logs to, default is /logs. The log file name will be ${program_name}.${hostname}.${username}.log")
	flagset.StringVar(&logFile, "zap-log-file", logFile, "file to write logs to, default is log.log.")
	flagset.StringVar(&logLevel, "zap-log-level", logLevel, "Log level, info or debug. Default to info.")
	flagset.IntVar(&maxSize, "zap-max-size", maxSize, "MaxSize is the maximum size in megabytes of the log file before it gets rotated. It defaults to 100 megabytes.")
	flagset.IntVar(&maxBackups, "zap-max-backups", maxBackups, "MaxBackups is the maximum number of old log files to retain.  The default is to retain all old log files (though MaxAge may still cause them to get deleted.) Default to 100.")
	flagset.IntVar(&maxAge, "zap-max-age", maxAge, "MaxAge is the maximum number of days to retain old log files based on the timestamp encoded in their filename. Default to 30.")
}

func Init() error {
	if err := validate(); nil != err {
		return err
	}

	level := levelMap[logLevel]

	curLogFile := filepath.Join(logDir, logFile)

	w := zapcore.AddSync(&lumberjack.Logger{
		Filename:   curLogFile,
		MaxSize:    maxSize, // megabytes
		MaxBackups: maxBackups,
		MaxAge:     maxAge, // days
		LocalTime:  true,
	})

	encoderConfig := zap.NewProductionEncoderConfig()
	if level == zapcore.DebugLevel {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
	}

	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stdout), w),
		level,
	)

	logger := zapr.NewLogger(zap.New(core))
	logf.SetLogger(logger)

	return nil
}
