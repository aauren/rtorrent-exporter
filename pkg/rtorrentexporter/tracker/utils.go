package tracker

import (
	"net/url"
)

func GetDomainForTrackerURL(urlString string) (string, error) {
	parsedURL, err := url.Parse(urlString)
	if err != nil {
		return "", err
	}
	return parsedURL.Hostname(), nil
}
