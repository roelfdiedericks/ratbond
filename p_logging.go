package main
import (
	"os"
	"fmt"
	"log"
	"log/syslog"
	"path"
	"runtime"
	"io"
	logrus "github.com/sirupsen/logrus"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var l *logrus.Logger

// FormatterHook is a hook that writes logs of specified LogLevels with a formatter to specified Writer
type FormatterHook struct {
	Writer    io.Writer
	LogLevels []logrus.Level
	Formatter logrus.Formatter
}

// Fire will be called when some logging function is called with current hook
// It will format log entry and write it to appropriate writer
func (hook *FormatterHook) Fire(entry *logrus.Entry) error {
	line, err := hook.Formatter.Format(entry)
	if err != nil {
		return err
	}
	_, err = hook.Writer.Write(line)
	return err
}

// Levels define on which log levels this hook would trigger
func (hook *FormatterHook) Levels() []logrus.Level {
	return hook.LogLevels
}

func init_logging() {
	l = logrus.New()

	if g_trace {
		l.Level = logrus.TraceLevel
	} else if g_debug {
		l.Level = logrus.DebugLevel
	} else {
		l.Level = logrus.InfoLevel
	}

	l.AddHook(&FormatterHook{ // Send logs with level higher than info to stderr
		Writer: os.Stderr,
		LogLevels: []logrus.Level{
			logrus.DebugLevel,
			logrus.TraceLevel,
			logrus.PanicLevel,
			logrus.FatalLevel,
			logrus.ErrorLevel,
			logrus.WarnLevel,
			logrus.InfoLevel,
		},
		Formatter: &logrus.TextFormatter{
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				if !g_debug && !g_trace {
					return "", f.Function+":: "
				}
				filename := path.Base(f.File)
				filename = filename + ""
				return fmt.Sprintf("%s:: ", f.Function), ""
				//return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", filename, f.Line)
			},
			ForceColors:            true,
			DisableTimestamp:       false,
			TimestampFormat:        "2006-01-02 15:04:05",
			DisableColors:          false,
			QuoteEmptyFields:       true,
			DisableLevelTruncation: true,
			PadLevelText:           true,
			FullTimestamp:          true,
			// Customizing delimiters
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "@timestamp",
				logrus.FieldKeyLevel: "severity",
				logrus.FieldKeyMsg:   "message",
				logrus.FieldKeyFunc:  "caller",
			},
		},
	})

	if (g_use_syslog) {
		syslogWriter, _ := syslog.New(syslog.LOG_INFO, "ratbond")
		l.AddHook(&FormatterHook{ // Send logs with level higher than info to stderr
			Writer: syslogWriter,
			LogLevels: []logrus.Level{
				logrus.PanicLevel,
				logrus.FatalLevel,
				logrus.ErrorLevel,
				logrus.WarnLevel,
				logrus.InfoLevel,
			},
			Formatter: &logrus.TextFormatter{
				CallerPrettyfier: func(f *runtime.Frame) (string, string) {
					if !g_debug && !g_trace {
						return "", ""
					}
					filename := path.Base(f.File)
					filename = filename + ""
					return fmt.Sprintf("%s()", f.Function), ""
					//return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", filename, f.Line)
				},
				ForceColors:            false,
				DisableTimestamp:       true,
				DisableColors:          true,
				QuoteEmptyFields:       true,
				DisableLevelTruncation: false,
				PadLevelText:           false,
				FullTimestamp:          false,
				// Customizing delimiters
				FieldMap: logrus.FieldMap{
					//logrus.FieldKeyTime:  "@timestamp",
					logrus.FieldKeyLevel: "severity",
					logrus.FieldKeyMsg:   "message",
					logrus.FieldKeyFunc:  "caller",
				},
			},
		})
	}

	l.SetOutput(io.Discard)
	l.SetReportCaller(true)
	/*hook, err := lSyslog.NewSyslogHook("", "", syslog.LOG_INFO, "")
	if err == nil {
		l.Hooks.Add(hook)
	}
	*/

	//l.Infof("Starting up...(debug:%t) (trace:%t)", g_debug, g_trace)
	w := l.Writer()

	mqtt.ERROR = log.New(w, "[MQTT:ERROR] ", log.Lshortfile)
	mqtt.CRITICAL = log.New(w, "[MQTT:CRIT] ", log.Lshortfile)
	mqtt.WARN = log.New(w, "[MQTT:WARN]  ", log.Lshortfile)
	//mqtt.DEBUG = log.New(w, "[MQTT:DEBUG] ", log.Lshortfile)
}