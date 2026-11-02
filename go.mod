module github.com/otuschhoff/rsc-go

go 1.26.0

require (
	github.com/rubrikinc/rubrik-polaris-sdk-for-go v1.11.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/golang-jwt/jwt/v4 v4.5.2 // indirect

replace github.com/rubrikinc/rubrik-polaris-sdk-for-go => ./third_party/rubrik-polaris-sdk-for-go
