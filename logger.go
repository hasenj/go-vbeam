package vbeam

import (
	"fmt"
	"log"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

func InitRotatingLogger(name string) {
	logger := &lumberjack.Logger{
		Filename:   fmt.Sprintf("logs/%s.log", name),
		MaxSize:    200, // megabytes
		MaxBackups: 10,
		MaxAge:     10, //days
		LocalTime:  true,
		// Compress:   true, // disabled by default
	}

	// Rotate logger on the 00:00 hour mark every day
	go func() {
		for {
			now := time.Now()
			nextDay := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
			duration := nextDay.Sub(now)
			time.Sleep(duration)
			logger.Rotate()
		}
	}()

	log.SetOutput(logger)
}
