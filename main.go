package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
	"unicode"

	"encoding/json"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/examples/lib/dev"
	"github.com/pkg/errors"

	"database/sql"
	database "database/sql"

	_ "github.com/mattn/go-sqlite3"

	"net/http"

	"github.com/labstack/echo/v4"
)

var mutex sync.RWMutex
var devices map[string]Device
var db *database.DB

type Device struct {
	Id       int       `json:"id"`
	Address  string    `json:"address"`
	Detected time.Time `json:"detected"`
	Name     string    `json:"name"`
	RSSI     int       `json:"rssi"`
}

type Bus struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

var (
	device = flag.String("device", "default", "implementation of ble")
	du     = flag.Duration("du", 10*time.Second, "scanning duration")
	delay  = flag.Duration("delay", 10*time.Second, "delay between scanning")
	dup    = flag.Bool("dup", false, "allow duplicate reported")
)

func init() {
	devices = make(map[string]Device)
	mutex = sync.RWMutex{}
}

func main() {
	flag.Parse()

	// Read watchlist file
	jsonFile, errWatchlist := os.Open("./watchlist.json")
	if errWatchlist != nil {
		log.Fatalln("File watchlist.json not found. You can copy from the template file")
		chkErr(errWatchlist)
	}
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)
	var watchlist map[string]Bus
	json.Unmarshal(byteValue, &watchlist)

	// Open sqlite file
	var errDb error
	db, errDb = sql.Open("sqlite3", "./ble.sqlite")
	if errDb != nil {
		log.Fatalln("Database ble.sqlite not found. You can copy from the template file")
		chkErr(errDb)
	}
	defer db.Close()

	// Initialize BLE device
	d, err := dev.NewDevice(*device)
	if err != nil {
		log.Fatalf("can't new device : %s", err)
	}
	ble.SetDefaultDevice(d)

	// Running loop
	go loop()
	go server()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	<-sigs
	os.Exit(0)
}

func loop() {
	for {
		// Scan for specified durantion, or until interrupted by user.
		fmt.Printf("Scanning for %s...\n", *du)
		ctx := ble.WithSigHandler(context.WithTimeout(context.Background(), *du))
		chkErr(ble.Scan(ctx, *dup, advHandler, nil))
		time.Sleep(*delay)
	}
}

func server() {
	e := echo.New()
	e.GET("/", func(c echo.Context) error {
		rows, err := db.Query("SELECT id, address, rssi, name, detected FROM beacon")
		var devList []Device
		for rows.Next() {
			device := Device{}
			err = rows.Scan(&device.Id, &device.Address, &device.RSSI, &device.Name, &device.Detected)
			chkErr(err)
			devList = append(devList, device)
		}
		rows.Close()
		chkErr(err)
		return c.JSON(http.StatusOK, devList)
	})
	e.Logger.Fatal(e.Start(":1323"))
}

func advHandler(a ble.Advertisement) {
	bleAddr := a.Addr().String()
	device := Device{
		Address:  bleAddr,
		Detected: time.Now(),
		Name:     clean(a.LocalName()),
		RSSI:     a.RSSI(),
	}
	mutex.Lock()
	isInsert := true
	if val, ok := devices[bleAddr]; ok {
		timediff := device.Detected.Sub(val.Detected).Minutes()
		// If last detected of same device < 1 min, then dont insert to DB
		if timediff < 1 {
			log.Println("Re-detected at : " + fmt.Sprint(timediff))
			// Update device name only for existing record
			if len(device.Name) > 0 {
				updateData(bleAddr, device)
			}
			isInsert = false
		}
	}
	devices[bleAddr] = device
	if isInsert {
		handleData(bleAddr, device)
	}
	mutex.Unlock()

	if a.Connectable() {
		fmt.Printf("[%s] C %3d:", a.Addr(), a.RSSI())
	} else {
		fmt.Printf("[%s] N %3d:", a.Addr(), a.RSSI())
	}
	/*
		comma := ""
		if len(a.LocalName()) > 0 {
			fmt.Printf(" Name: %s", a.LocalName())
			comma = ","
		}
		if len(a.Services()) > 0 {
			fmt.Printf("%s Svcs: %v", comma, a.Services())
			comma = ","
		}
		if len(a.ManufacturerData()) > 0 {
			fmt.Printf("%s MD: %X", comma, a.ManufacturerData())
		}*/
	fmt.Printf("\n")
}

func handleData(bleAddr string, device Device) {

	stmt, err := db.Prepare("INSERT INTO beacon(detected, address, rssi, name) values(?,?,?,?)")
	chkErr(err)

	res, err := stmt.Exec(device.Detected, device.Address, device.RSSI, device.Name)
	chkErr(err)

	log.Print("Inserted : ")
	log.Println(res.LastInsertId())

}

func updateData(bleAddr string, device Device) {

	stmt, err := db.Prepare("UPDATE beacon set name=? WHERE address=?")
	chkErr(err)

	res, err := stmt.Exec(device.Name, device.Address)
	chkErr(err)

	log.Print("Updated : ")
	log.Println(res.LastInsertId())

}

func chkErr(err error) {
	switch errors.Cause(err) {
	case nil:
	case context.DeadlineExceeded:
		fmt.Printf("done\n")
	case context.Canceled:
		fmt.Printf("canceled\n")
	default:
		log.Fatalf(err.Error())
		os.Exit(1)
	}
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

// reformat string for proper display of hex
func formatHex(instr string) (outstr string) {
	outstr = ""
	for i := range instr {
		if i%2 == 0 {
			outstr += instr[i:i+2] + " "
		}
	}
	return
}

// clean up the non-ASCII characters
func clean(input string) string {
	return strings.TrimFunc(input, func(r rune) bool {
		return !unicode.IsGraphic(r)
	})
}
