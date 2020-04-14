package tools

import "fmt"

//colors for terminal ouput
const (
	InfoColor    = "\033[1;34m"
	NoticeColor  = "\033[1;36m"
	WarningColor = "\033[1;33m"
	ErrorColor   = "\033[1;31m"
	DebugColor   = "\033[0;36m"
)

func printf(prefixcolor string, format string, a ...interface{}) (n int, err error) {
	return fmt.Printf(prefixcolor+format+"\033[0m", a...)

}

//InfoPrintf print an info txt
func InfoPrintf(format string, a ...interface{}) (n int, err error) {
	return printf(InfoColor, format, a...)
}

//NoticePrintf print an notice txt
func NoticePrintf(format string, a ...interface{}) (n int, err error) {
	return printf(NoticeColor, format, a...)
}

//WarningPrintf print a warning txt
func WarningPrintf(format string, a ...interface{}) (n int, err error) {
	return printf(WarningColor, format, a...)
}

//ErrorPrintf print an error txt
func ErrorPrintf(format string, a ...interface{}) (n int, err error) {
	return printf(ErrorColor, format, a...)
}

//DebugPrintf print a debug txt
func DebugPrintf(format string, a ...interface{}) (n int, err error) {
	return printf(DebugColor, format, a...)
}
