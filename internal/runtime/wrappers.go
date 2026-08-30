package runtime

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

const DefaultWrapperDepth = 16

var (
	ErrWrapperDepth       = errors.New("runtime: MTProto wrapper depth exceeded")
	ErrInvalidNestedQuery = errors.New("runtime: invalid nested TL query")
	ErrInvalidLayer       = errors.New("runtime: invalid requested schema layer")
)

type WrapperConfig struct {
	SchemaLayer       int
	MaxDecodedPayload int
	MaxDepth          int
}

// NormalizeRequest removes runtime-owned MTProto wrappers before generated
// application dispatch and returns only named durable session mutations.
func NormalizeRequest(request Request, config WrapperConfig) (Request, []SessionMutation, error) {
	maxDepth := config.MaxDepth
	if maxDepth == 0 {
		maxDepth = DefaultWrapperDepth
	}
	if maxDepth < 0 {
		return Request{}, nil, ErrWrapperDepth
	}
	mutations := make([]SessionMutation, 0, 2)
	for depth := 0; depth < maxDepth; depth++ {
		body := request.Message.Body
		if len(body) < 4 {
			return Request{}, nil, ErrInvalidNestedQuery
		}
		constructorID := binary.LittleEndian.Uint32(body[:4])
		var nested []byte
		switch constructorID {
		case mtprototl.GzipPackedID:
			wrapper := &mtprototl.GzipPacked{}
			if err := decodeControl(body, wrapper); err != nil {
				return Request{}, nil, err
			}
			decoded, err := mtproto.DecompressGzip(wrapper.PackedData, config.MaxDecodedPayload)
			if err != nil {
				return Request{}, nil, err
			}
			nested = decoded

		case mtprototl.InvokeWithLayerID:
			wrapper := &mtprototl.InvokeWithLayer{}
			if err := decodeControl(body, wrapper); err != nil {
				return Request{}, nil, err
			}
			layer := int(wrapper.Layer)
			if layer <= 0 {
				return Request{}, nil, ErrInvalidLayer
			}
			if config.SchemaLayer > 0 && layer > config.SchemaLayer {
				layer = config.SchemaLayer
			}
			mutations = append(mutations, SetLayer{Layer: layer})
			nested = wrapper.QueryRaw

		case mtprototl.InitConnectionID:
			wrapper := &mtprototl.InitConnection{}
			if err := decodeControl(body, wrapper); err != nil {
				return Request{}, nil, err
			}
			mutations = append(mutations, SetClientMetadata{
				APIID: wrapper.APIID, DeviceModel: wrapper.DeviceModel,
				SystemVersion: wrapper.SystemVersion, AppVersion: wrapper.AppVersion,
				SystemLangCode: wrapper.SystemLangCode, LangPack: wrapper.LangPack,
				LangCode: wrapper.LangCode,
			})
			nested = wrapper.QueryRaw

		case mtprototl.InvokeAfterMsgID:
			wrapper := &mtprototl.InvokeAfterMsg{}
			if err := decodeControl(body, wrapper); err != nil {
				return Request{}, nil, err
			}
			nested = wrapper.QueryRaw

		case mtprototl.InvokeAfterMsgsID:
			wrapper := &mtprototl.InvokeAfterMsgs{}
			if err := decodeControl(body, wrapper); err != nil {
				return Request{}, nil, err
			}
			nested = wrapper.QueryRaw

		case mtprototl.InvokeWithoutUpdatesID:
			wrapper := &mtprototl.InvokeWithoutUpdates{}
			if err := decodeControl(body, wrapper); err != nil {
				return Request{}, nil, err
			}
			request.Message.SuppressPush = true
			nested = wrapper.QueryRaw

		default:
			request.Message.ConstructorID = constructorID
			request.Message.Body = append([]byte(nil), body...)
			return request, mutations, nil
		}
		if len(nested) < 4 {
			return Request{}, nil, ErrInvalidNestedQuery
		}
		request.Message.Body = append([]byte(nil), nested...)
		request.Message.ConstructorID = binary.LittleEndian.Uint32(nested[:4])
	}
	return Request{}, nil, fmt.Errorf("%w: limit %d", ErrWrapperDepth, maxDepth)
}

func suppressPushIntents(intents []Intent) []Intent {
	filtered := make([]Intent, 0, len(intents))
	for _, intent := range intents {
		switch value := intent.(type) {
		case Push:
			continue
		case Batch:
			children := suppressPushIntents(value.Items)
			if len(children) == 0 {
				continue
			}
			filtered = append(filtered, Batch{Items: children})
		default:
			filtered = append(filtered, intent)
		}
	}
	return filtered
}
