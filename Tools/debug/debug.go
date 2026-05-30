package debug

import (
	"log"
	"runtime"
)

func W(v ...any) {
	_, file, line, ok := runtime.Caller(1)
	if ok {
		log.Printf("[WARN] %s:%d %v", file, line, v)
	} else {
		log.Printf("[WARN] %v", v)
	}
}

func I(v ...any) {
	_, file, line, ok := runtime.Caller(1)
	if ok {
		log.Printf("[INFO] %s:%d %v", file, line, v)
	} else {
		log.Printf("[INFO] %v", v)
	}
}
