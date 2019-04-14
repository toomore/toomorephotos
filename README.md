1. go get -v -a ./...
2. go build ./
3. Download [`unveil.js`](https://github.com/luis-almeida/unveil) 
4. (minify css, js) go get github.com/tdewolff/minify/v2 
5. setup `FLICKRAPIKEY`, `FLICKRSECRET`, `FLICKRUSERTOKEN`, `FLICKRUSER`
6. ./toomorephotos >> ./log.log 2>&1 &
