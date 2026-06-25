package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type Logger struct {
	out      io.Writer
	level    string
	fields   map[string]interface{}
}

type LogEntry struct {
	Time    string                 `json:"time"`
	Level   string                 `json:"level"`
	Message string                 `json:"message"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
}

func NewLogger(out io.Writer, level string) *Logger {
	return &Logger{
		out:   out,
		level: level,
	}
}

func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	return &Logger{
		out:    l.out,
		level:  l.level,
		fields: newFields,
	}
}

func (l *Logger) log(level, message string) {
	if l.level == "" {
		l.level = "info"
	}

	levelOrder := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
	}

	if levelOrder[level] < levelOrder[l.level] {
		return
	}

	entry := LogEntry{
		Time:    time.Now().Format(time.RFC3339),
		Level:   level,
		Message: message,
		Fields:  l.fields,
	}

	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.out, string(data))
}

func (l *Logger) Debug(msg string) {
	l.log("debug", msg)
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log("debug", fmt.Sprintf(format, args...))
}

func (l *Logger) Info(msg string) {
	l.log("info", msg)
}

func (l *Logger) Infof(format string, args ...interface{}) {
	l.log("info", fmt.Sprintf(format, args...))
}

func (l *Logger) Warn(msg string) {
	l.log("warn", msg)
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log("warn", fmt.Sprintf(format, args...))
}

func (l *Logger) Error(msg string) {
	l.log("error", msg)
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log("error", fmt.Sprintf(format, args...))
}

func NewStdoutLogger(level string) *Logger {
	return NewLogger(os.Stdout, level)
}
