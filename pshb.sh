curl -v "https://pubsubhubbub.appspot.com/" \
     -H "Content-Type: application/x-www-form-urlencoded" \
     -d "hub.mode=publish" \
     -d "hub.url=https://photos.toomore.net/rss"
