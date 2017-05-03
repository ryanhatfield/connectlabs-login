package app

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/jinzhu/gorm"
	//This is required for the postgres driver within gorm
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/ryanhatfield/connectlabs-login/ap"
	"github.com/ryanhatfield/connectlabs-login/sso"
)

type Data struct {
	DatabaseURL      string
	Debug            bool
	MaxDBConnections int
	db               *gorm.DB
}

func (d *Data) InitializeDB() error {
	log.Println("Initializing DB adapter")
	db, err := gorm.Open("postgres", d.DatabaseURL)
	if err != nil {
		return err
	}

	db.DB().SetMaxOpenConns(d.MaxDBConnections)
	db.DB().SetMaxIdleConns(d.MaxDBConnections)
	db.AutoMigrate(&ap.Session{})
	db.AutoMigrate(&sso.User{})
	db.AutoMigrate(&sso.UserLogin{})

	if d.Debug {
		d.db = db.Debug()
	} else {
		d.db = db
	}

	return nil
}

func (d *Data) FindSession(session *ap.Session, sessionID string) error {
	if sessionID == "" {
		return errors.New("session id empty")
	}

	log.Printf("session: %+v\n", *session)
	db := d.db
	if db == nil {
		return errors.New("issue with nil db")
	}

	db.Where("token = ?", sessionID).Find(&session)

	return nil
}

func (d *Data) UpdateSession(req *ap.Request) error {
	session := &ap.Session{}
	d.FindSession(session, req.Session)

	session.IPv4 = req.IPV4Address
	if req.RequestType == ap.RequestTypeAccounting {
		getint := func(s string) uint {
			u, _ := strconv.ParseUint(req.Download, 0, 32)
			return uint(u)
		}
		session.Download = getint(req.Download)
		session.Upload = getint(req.Upload)
		session.Seconds = getint(req.Seconds)
	}

	if d.db.NewRecord(session) {
		//session not found, save a new instance
		session.Token = req.Session
		session.Device = req.MacAddress
		session.Node = req.NodeAddress
		session.ExpiresAt = time.Now()
		d.db.Save(&session)
	} else {
		d.db.Model(&session).Updates(ap.Session{
			Download: session.Download,
			Upload:   session.Upload,
			IPv4:     session.IPv4,
			Seconds:  session.Seconds,
		})
	}

	return nil
}

func (d *Data) FindUserByID(user *sso.User, userID uint) error {

	d.db.First(user, struct{ ID uint }{ID: userID})

	return nil
}

func (d *Data) AddLoginToUser(userID uint, login sso.UserLogin) error {
	user := &sso.User{}

	d.FindUserByID(user, userID)
	d.db.Create(login)
	//make sure it saved
	if !d.db.NewRecord(login) {
		return errors.New("error associating login with user.")
	}

	user.Logins = append(user.Logins, login)
	d.db.Save(&user)
	return nil
}

func (d *Data) FindUserID(session, ip, mac string) string {
	s := &ap.Session{}

	d.db.Find(s, struct{ Token string }{Token: session})

	if !d.db.NewRecord(s) {
		return fmt.Sprint(s.UserID)
	}

	d.db.Find(s, struct{ IPv4, Device string }{ip, mac})

	if !d.db.NewRecord(s) {
		return fmt.Sprint(s.UserID)
	}

	return ""
}

func (d *Data) FindUserByDevice(user *sso.User, ip string, mac string) error {
	u := &sso.User{}
	s := &ap.Session{}

	d.db.Where("ipv4 = ? AND device = ?", ip, mac).Order("expires_at desc").First(s)
	if s != nil && !d.db.NewRecord(s) {
		d.FindUserByID(u, s.UserID)
		if !d.db.NewRecord(u) {
			user = u
			return nil
		}
		return fmt.Errorf("could not find user with user id: %d", s.UserID)
	}
	return fmt.Errorf("could not find user with ip: %s, mac: %s", ip, mac)
}
