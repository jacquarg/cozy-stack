## cozy-stack doc

Print the documentation

### Synopsis


Print the documentation about the usage of cozy-stack in command-line

```
cozy-stack doc [command]
```

### Options inherited from parent commands

```
      --admin-host string      administration server host (default "localhost")
      --admin-port int         administration server port (default 6060)
      --assets string          path to the directory with the assets (use the packed assets by default)
  -c, --config string          configuration file (default "$HOME/.cozy.yaml")
      --couchdb-url string     CouchDB URL (default "http://localhost:5984/")
      --fs-url string          filesystem url (default "file://localhost//storage")
      --host string            server host (default "localhost")
      --log-level string       define the log level (default "info")
      --mail-disable-tls       disable smtp over tls
      --mail-host string       mail smtp host (default "localhost")
      --mail-password string   mail smtp password
      --mail-port int          mail smtp port (default 465)
      --mail-username string   mail smtp username
  -p, --port int               server port (default 8080)
      --subdomains string      how to structure the subdomains for apps (can be nested or flat) (default "nested")
```

### SEE ALSO
* [cozy-stack](cozy-stack.md)	 - cozy-stack is the main command
* [cozy-stack doc man](cozy-stack_doc_man.md)	 - Print the manpages of cozy-stack
* [cozy-stack doc markdown](cozy-stack_doc_markdown.md)	 - Print the documentation of cozy-stack as markdown
