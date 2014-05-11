package main

import (
    "code.google.com/p/go.net/html"
    "encoding/json"
    log "github.com/cihub/seelog"
    "github.com/thoj/go-ircevent"
    "io"
    "io/ioutil"
    "net/http"
    "os"
    "regexp"
    "strings"
)

type Configuration struct {
    Server       string
    SSL          bool
    Nick         string
    Username     string
    RoomName     string
    IgnoreRegex  string
    Debug        bool
    HelloMessage string
}

type URLHistory struct {
    Id   int
    URL  string
    Date string
    Who  string
}

func init() {
    logger, err := log.LoggerFromConfigAsFile("logging.xml")

    if err != nil {
        log.Debug(err)
        return
    }

    log.ReplaceLogger(logger)
    return
}

func main() {
    defer log.Flush()
    // Load Configuration
    file, _ := os.Open("conf.json")
    decoder := json.NewDecoder(file)
    conf := Configuration{}
    err := decoder.Decode(&conf)
    if err != nil {
        log.Debug("Error reading config file: ", err)
        return
    }

    log.Info("Starting up, connecting to ", conf.Server, " as ", conf.Nick)

    // Connect to IRC server
    con := irc.IRC(conf.Nick, conf.Username)
    con.Debug = conf.Debug
    con.UseTLS = conf.SSL
    err = con.Connect(conf.Server)

    if err != nil {
        log.Debug("Failed connection: ", err)
        return
    }

    log.Info("Connected: ", conf.Server)
    // When we've connected to the IRC server, go join the room!
    con.AddCallback("001", func(e *irc.Event) {
        con.Join(conf.RoomName)
        log.Info("Joined room ", conf.RoomName)
    })

    if conf.HelloMessage != "" {
        // Say something on arrival
        con.AddCallback("JOIN", func(e *irc.Event) {
            con.Privmsg(conf.RoomName, conf.HelloMessage)
        })
    }
    // Check each message to see if it contains a URL, and return the title
    con.AddCallback("PRIVMSG", func(e *irc.Event) {
        // Regex to catch web URLs.
        var webaddress = regexp.MustCompile("http(s)?\\S*")
        var ignoreRegex = regexp.MustCompile(conf.IgnoreRegex)

        ignoreIt := ignoreRegex.FindString(strings.ToLower(e.Message()))

        if ignoreIt == "" {
            matched := webaddress.FindString(e.Message())

            if matched != "" {
                // We found a URL.  Fetch the page
                resp, err := http.Get(matched)
                if err != nil {
                    log.Debug(err)
                } else {
                    // Wait until finished getting the content
                    defer resp.Body.Close()
                    contents, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<18))
                    if err != nil {
                        log.Debug(err)
                    } else if resp.StatusCode != 200 {
                        log.Debug(matched, "returned with status code:", resp.Status)
                    } else {
                        // Find the title from the page, log and send it to IRC channel
                        title := getTitle(string(contents))
                        log.Debug("Went to ", matched, " got title ", title)
                        con.Privmsg(conf.RoomName, title)
                    }
                }
            }
        } else {
            log.Info("Message is from ignored user, ", ignoreIt)
        }
    })
    // Let's just keep on keeping on
    con.Loop()
}

func getTitle(body string) string {
    // Setting a default title
    title := "No Title Found"
    // Splits the html up into a series of tokens (tags)
    d := html.NewTokenizer(strings.NewReader(body))
    for {
        tokenType := d.Next()
        if tokenType == html.ErrorToken {
            return title
        }
        token := d.Token()
        if tokenType == html.StartTagToken {
            if strings.ToLower(token.Data) == "title" {
                tokenType := d.Next()
                if tokenType == html.TextToken {
                    return d.Token().Data
                }
            }
        }
    }
    return title
}
