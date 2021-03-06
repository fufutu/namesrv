package conf

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"bytes"

	"github.com/simplejia/clog"
	"github.com/simplejia/utils"
)

type Conf struct {
	App *struct {
		Name string
		Port int
	}
	Clog *struct {
		Name  string
		Mode  int
		Level int
	}
	VarHost *struct {
		Addr string
		Cgi  string
	}
}

func Get() *Conf {
	return C.Load().(*Conf)
}

func Set(c *Conf) {
	C.Store(c)
}

var (
	Env      string
	UserConf string
	C        atomic.Value
)

func remoteConf() {
	c := Get()
	if c.VarHost == nil || c.VarHost.Addr == "" || c.VarHost.Cgi == "" {
		return
	}

	go func() {
		lastbody := []byte{}
		for {
			time.Sleep(time.Second * 3)

			uri := fmt.Sprintf("http://%s:%d/%s", c.VarHost.Addr, c.App.Port, strings.TrimPrefix(c.VarHost.Cgi, "/"))
			gpp := &utils.GPP{
				Uri: uri,
			}
			body, err := utils.Get(gpp)
			if err != nil {
				clog.Error("conf.remoteConf() http error, err: %v, body: %s, gpp: %v", err, body, gpp)
				continue
			}

			if len(body) == 0 {
				continue
			}

			if len(lastbody) != 0 && bytes.Compare(lastbody, body) == 0 {
				continue
			}

			if err := parse(body); err != nil {
				clog.Error("conf.remoteConf() parse error, err: %v, body: %s", err, body)
				continue
			}

			if err := savecontents(body); err != nil {
				clog.Error("conf.remoteConf() savecontents error, err: %v, body: %s", err, body)
				continue
			}

			lastbody = body
		}
	}()
}

func getcontents() (fcontent []byte, err error) {
	dir := "conf"
	for i := 0; i < 3; i++ {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			break
		}
		dir = filepath.Join("..", dir)
	}
	fcontent, err = ioutil.ReadFile(filepath.Join(dir, "conf.json"))
	if err != nil {
		return
	}
	return
}

func savecontents(fcontent []byte) (err error) {
	dir := "conf"
	for i := 0; i < 3; i++ {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			break
		}
		dir = filepath.Join("..", dir)
	}
	err = ioutil.WriteFile(filepath.Join(dir, "conf.json"), fcontent, 0644)
	if err != nil {
		return
	}
	return
}

func parse(fcontent []byte) (err error) {
	fcontent = utils.RemoveAnnotation(fcontent)

	var envs map[string]*Conf
	if err = json.Unmarshal(fcontent, &envs); err != nil {
		return
	}

	c := envs[Env]
	if c == nil {
		return fmt.Errorf("env not right: %s", Env)
	}

	matchs := regexp.MustCompile(`[\w|\.]+|".*?[^\\"]"`).FindAllString(UserConf, -1)
	for n, match := range matchs {
		matchs[n] = strings.Replace(strings.Trim(match, "\""), `\"`, `"`, -1)
	}
	for n := 0; n < len(matchs); n += 2 {
		name, value := matchs[n], matchs[n+1]

		rv := reflect.Indirect(reflect.ValueOf(c))
		for _, field := range strings.Split(name, ".") {
			rv = reflect.Indirect(rv.FieldByName(strings.Title(field)))
		}
		switch rv.Kind() {
		case reflect.String:
			rv.SetString(value)
		case reflect.Bool:
			b, err := strconv.ParseBool(value)
			if err != nil {
				return err
			}
			rv.SetBool(b)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			rv.SetInt(i)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}
			rv.SetUint(u)
		case reflect.Float32, reflect.Float64:
			f, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return err
			}
			rv.SetFloat(f)
		}
	}

	// init log
	clog.Init(c.Clog.Name, "", c.Clog.Level, c.Clog.Mode)

	Set(c)

	log.Printf("Env: %s\nC: %s\n", Env, utils.Iprint(c))
	return
}

func Cgi(w http.ResponseWriter, r *http.Request) {
	fun := "conf.Cgi"

	fcontent, err := getcontents()
	if err != nil {
		log.Printf("%s read file error: %v", fun, err)
		return
	}

	w.Write(fcontent)
	return
}

func init() {
	flag.StringVar(&Env, "env", "prod", "set env")
	flag.StringVar(&UserConf, "conf", "", "set custom conf")
	flag.Parse()

	fcontent, err := getcontents()
	if err != nil {
		log.Printf("get conf file contents error: %v\n", err)
		os.Exit(-1)
	}
	err = parse(fcontent)
	if err != nil {
		log.Printf("parse conf file error: %v\n", err)
		os.Exit(-1)
	}

	remoteConf()
}
