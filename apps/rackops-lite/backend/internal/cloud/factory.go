package cloud

import (
	"context"
	"encoding/json"
	"errors"
)

func NewProvider(ctx context.Context, name, region string, raw json.RawMessage) (Provider, error) {
	switch name {
	case "aws":
		var c AWSCredentials
		if json.Unmarshal(raw, &c) != nil {
			return nil, errors.New("invalid AWS credentials")
		}
		return NewAWS(region, c)
	case "azure":
		var c AzureCredentials
		if json.Unmarshal(raw, &c) != nil {
			return nil, errors.New("invalid Azure credentials")
		}
		return NewAzure(region, c)
	case "gcp":
		var c GCPCredentials
		if json.Unmarshal(raw, &c) != nil {
			return nil, errors.New("invalid Google credentials")
		}
		return NewGCP(ctx, region, c)
	}
	return nil, errors.New("unsupported cloud provider")
}
