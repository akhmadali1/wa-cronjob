package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
	"wa-auto/model"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	_ "github.com/mattn/go-sqlite3"
	"github.com/robfig/cron/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var client *whatsmeow.Client

func main() {
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New("sqlite3", "file:otpdbtemp.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client = whatsmeow.NewClient(deviceStore, clientLog)

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("QR code:", evt.Code)
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		fmt.Println("Error loading location:", err)
		return
	}

	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.GET("/kalbe/morning", func(c echo.Context) error {
		return hello(c, client, loc, 1)
	})

	e.GET("/kalbe/night", func(c echo.Context) error {
		return hello(c, client, loc, 2)
	})

	// Start server in a goroutine
	go func() {
		if err := e.Start(":1323"); err != nil {
			e.Logger.Fatal(err)
		}
	}()

	go monitorConnection()

	scheduler := cron.New(cron.WithLocation(loc))

	// stop scheduler tepat sebelum fungsi berakhir
	defer scheduler.Stop()

	// set task yang akan dijalankan scheduler
	// gunakan crontab string untuk mengatur jadwal
	scheduler.AddFunc("0 07 * * 1-5", func() { hitRoutineService("kalbe/morning") })
	scheduler.AddFunc("0 20 * * *", func() { hitRoutineService("kalbe/night") })

	// start scheduler
	go scheduler.Start()

	// Use a buffered channel for signals to prevent SA1017 warning
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Use a simple channel receive instead of select with a single case (S1000)
	sig := <-signalChan
	fmt.Printf("Received signal: %v\n", sig)
	client.Disconnect()
	os.Exit(0)
}

func hello(c echo.Context, client *whatsmeow.Client, loc *time.Location, dayNight int) error {
	infoGroup, err := client.GetGroupInfoFromLink("https://chat.whatsapp.com/JU0uMNWKCSI3v0ZCqp2hKu")
	if err != nil {
		fmt.Println("Error get info group:", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"Message": "Failed to get info group"})
	}

	timeNow := time.Now().In(loc)

	t1 := Date(loc, timeNow.Year(), int(timeNow.Month()), timeNow.Day())
	t2 := Date(loc, 2024, 2, 16)
	days := t2.Sub(t1).Hours() / 24

	shuffledQuotes := shuffleQuotes(model.GetQuotesData(dayNight))

	index := int(days)
	if index < 0 || index >= len(shuffledQuotes) {
		index = len(shuffledQuotes) - 1
	}

	stringTemplate := fmt.Sprintf("Tersisa *%d* hari lagi,\n"+
		"%s", int(days), shuffledQuotes[index])

	if _, err := client.SendMessage(context.Background(), infoGroup.JID, &waProto.Message{
		Conversation: proto.String(stringTemplate),
	}); err != nil {
		fmt.Println("Error sending message:", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"Message": "Failed to send message"})
	}

	return c.JSON(http.StatusOK, echo.Map{"Message": "Success"})
}

func monitorConnection() {
	for {
		time.Sleep(time.Second * 10)
		if !client.IsConnected() {
			fmt.Println("Connection lost. Restarting service...")
			restartService()
		}
	}
}

func restartService() {
	cmd := exec.Command("sudo", "service", "wa-auto", "restart")
	err := cmd.Run()
	if err != nil {
		fmt.Println("Error restarting service:", err)
	}
}

func hitRoutineService(dayNight string) {
	urlHit := fmt.Sprintf("localhost:1323/%s", dayNight)
	cmd := exec.Command("curl", "-v", "-X", "GET", urlHit, "-H", "Content-Type: application/json")
	err := cmd.Run()
	if err != nil {
		fmt.Println("Error hitting routine service:", err)
	}
}

func Date(loc *time.Location, year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
}

func shuffleQuotes(quotes []string) []string {
	rand.Seed(time.Now().UnixNano())
	shuffledQuotes := make([]string, len(quotes))
	perm := rand.Perm(len(quotes))
	for i, j := range perm {
		shuffledQuotes[i] = quotes[j]
	}
	return shuffledQuotes
}

// func routinesConnectionMorning(loc *time.Location) {
// 	for {
// 		// Get the current time in Asia/Jakarta timezone
// 		timeNow := time.Now().In(loc)

// 		// Set the target time to 06:59 AM in Asia/Jakarta timezone
// 		targetTime := time.Date(timeNow.Year(), timeNow.Month(), timeNow.Day(), 6, 59, 0, 0, loc)

// 		// Calculate the duration until the target time
// 		duration := targetTime.Sub(timeNow)

// 		// Sleep until the target time
// 		time.Sleep(duration)

// 		// Perform the routine service if client is connected
// 		if client.IsConnected() {
// 			hitRoutineService()
// 		}
// 	}
// }

// func routinesConnectionNight(loc *time.Location) {
// 	for {
// 		// Get the current time in Asia/Jakarta timezone
// 		timeNow := time.Now().In(loc)

// 		// Set the target time to 06:59 AM in Asia/Jakarta timezone
// 		targetTime := time.Date(timeNow.Year(), timeNow.Month(), timeNow.Day(), 19, 59, 0, 0, loc)

// 		// Calculate the duration until the target time
// 		duration := targetTime.Sub(timeNow)

// 		// Sleep until the target time
// 		time.Sleep(duration)

// 		// Perform the routine service if client is connected
// 		if client.IsConnected() {
// 			hitRoutineService()
// 		}
// 	}
// }
