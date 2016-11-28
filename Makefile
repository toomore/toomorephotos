build:
	GOOS=linux GOARCH=amd64 go build -o toomorephotos_min -ldflags "-s" ./main.go

minify:
	minify -o ./jquery.unveil.min.js ./jquery.unveil.js
	minify -o ./base_min.css ./base.css

stop:
	ps aux | grep ./toomorephotos_min | awk {'print $$2'} | xargs sudo kill -9

start:
	./toomorephotos_min >> ./log.log 2>&1 &

restart:
	- make stop
	- make start
