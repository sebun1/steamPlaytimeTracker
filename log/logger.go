package log

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

const (
	D_DEBUG = iota
	D_INFO  = iota
	D_WARN  = iota
	D_ERROR = iota
	D_FATAL = iota
)

const (
	color_RESET  = "\033[0m"
	color_RED    = "\033[31m"
	color_GREEN  = "\033[32m"
	color_YELLOW = "\033[33m"
	color_BLUE   = "\033[34m"
	color_PURPLE = "\033[35m"
	color_CYAN   = "\033[36m"
	color_WHITE  = "\033[37m"
)

var label = map[int]string{
	D_DEBUG: color_BLUE + "[DEBUG] " + color_RESET,
	D_INFO:  color_GREEN + " [INFO] " + color_RESET,
	D_WARN:  color_YELLOW + " [WARN] " + color_RESET,
	D_ERROR: color_RED + "[ERROR] " + color_RESET,
	D_FATAL: color_RED + "[FATAL] " + color_RESET,
}

var log_level = D_INFO

func SetLevel(level int) {
	log_level = level
}

func SetLevelFromString(level string) {
	level = strings.ToUpper(level)
	switch level {
	case "DEBUG":
		log_level = D_DEBUG
	case "INFO":
		log_level = D_INFO
	case "WARN":
		log_level = D_WARN
	case "ERROR":
		log_level = D_ERROR
	case "FATAL":
		log_level = D_FATAL
	default:
		log_level = D_INFO
	}
}

func std_print(level int, v ...interface{}) {
	if level >= log_level {
		fmt.Print(time.Now().Format("2006-01-02 15:04:05.000") +
			" " + label[level])
		fmt.Println(v...)
	}
}

func Debug(v ...interface{}) {
	std_print(D_DEBUG, append([]interface{}{Trace()}, v...)...)
}

func Info(v ...interface{}) {
	std_print(D_INFO, v...)
}

func Warn(v ...interface{}) {
	std_print(D_WARN, v...)
}

func Error(v ...interface{}) {
	std_print(D_ERROR, append([]interface{}{Trace()}, v...)...)
}

func Fatal(v ...interface{}) {
	std_print(D_FATAL, append([]interface{}{Trace()}, v...)...)
}

func Debugf(format string, v ...interface{}) {
	std_print(D_DEBUG, append([]interface{}{Trace()}, fmt.Sprintf(format, v...))...)
}

func Infof(format string, v ...interface{}) {
	std_print(D_INFO, fmt.Sprintf(format, v...))
}

func Warnf(format string, v ...interface{}) {
	std_print(D_WARN, fmt.Sprintf(format, v...))
}

func Errorf(format string, v ...interface{}) {
	std_print(D_ERROR, append([]interface{}{Trace()}, fmt.Sprintf(format, v...))...)
}

func Fatalf(format string, v ...interface{}) {
	std_print(D_FATAL, append([]interface{}{Trace()}, fmt.Sprintf(format, v...))...)
}

func Trace() string {
	frame, ok := getFrame(1)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s[%s:%d %s]%s", color_PURPLE, frame.File, frame.Line, frame.Function, color_RESET)
}

func getFrame(skip int) (frame runtime.Frame, ok bool) {
	pc := make([]uintptr, 15)
	n := runtime.Callers(3+skip, pc)
	if n == 0 {
		return
	}
	frames := runtime.CallersFrames(pc[:n])
	frame, _ = frames.Next()
	return frame, true
}
