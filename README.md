<<<<<<< HEAD
go-bot
======

Go based IRC Bot
=======
This is an example IRC bot created using golang.

The primary purpose for this is to help me learn to use Google's Go programming language.  Whilst it is starting out from simple roots, I'm hoping to keep expanding it with more and more features, aiming to create an effective and powerful ircbot.

All configuration is handled through the conf.json file, to try to make things as easy as possible, an example of which is included in the repository.

Current state:

* Connects to a configured irc server and channel (only to single channel at the moment).
* Checks every message to see if it contains an http(s) URL, connects to the site, reads the title, and then tells the channel what it is.
* Ignores any messages by itself.

Features I'm hoping to add:

* Support for multiple IRC channels, handled by different go threads.
* Database back-end, trying to keep this as generic as possible so people can grab whatever db driver they want and use it.
* 'plug-in' support.  I believe these will need compiled in to the ircbot executable to run, but I'd like to shift the main IRC bot commands and functions out to plugins to make it easy to add new ones whilst keeping main code clean
* Better support for ignoring users.  Regexes are powerful things, but easy to get wrong.
* Per-user rate limiting (and maybe at bot level?)
>>>>>>> 5fb584a... Initial commit
