package runtime

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

const DefaultWrapperDepth = 16
const MaxInvokeAfterDependencies = 64

var (
	ErrWrapperDepth       = errors.New("runtime: MTProto wrapper depth exceeded")
	ErrInvalidNestedQuery = errors.New("runtime: invalid nested TL query")
	ErrInvalidLayer       = errors.New("runtime: invalid requested schema layer")
	ErrInvalidInvokeAfter = errors.New("runtime: invalid invoke-after dependency")
)

type WrapperConfig struct {
	SchemaLayer       int
	MaxDecodedPayload int
	MaxDepth          int
}

// NormalizeRequest removes runtime-owned MTProto wrappers before generated
// application dispatch and returns only named durable session mutations.
func NormalizeRequest(request Request, config WrapperConfig) (Request, []SessionMutation, error) {
	unlockBudget := request.Message.DecodeBudget.LockDecode()
	defer unlockBudget()
	maxDepth := config.MaxDepth
	if maxDepth == 0 {
		maxDepth = DefaultWrapperDepth
	}
	if maxDepth < 0 {
		return Request{}, nil, ErrWrapperDepth
	}
	mutations := make([]SessionMutation, 0, 2)
	for depth := 0; depth <= maxDepth; depth++ {
		body := request.Message.Body
		if len(body) < 4 {
			return Request{}, nil, ErrInvalidNestedQuery
		}
		constructorID := binary.LittleEndian.Uint32(body[:4])
		if depth == maxDepth && runtimeWrapperConstructor(constructorID) {
			return Request{}, nil, fmt.Errorf("%w: limit %d", ErrWrapperDepth, maxDepth)
		}
		if request.Message.DecodeBudget != nil && runtimeWrapperConstructor(constructorID) {
			reader := mtproto.NewBudgetReader(bytes.NewReader(nil), request.Message.DecodeBudget)
			if err := mtproto.ConsumeWrapper(reader); err != nil {
				return Request{}, nil, err
			}
		}
		var nested []byte
		switch constructorID {
		case mtprototl.GzipPackedID:
			wrapper := &mtprototl.GzipPacked{}
			if err := decodeControlBudget(body, wrapper, request.Message.DecodeBudget); err != nil {
				return Request{}, nil, err
			}
			var decoded []byte
			var err error
			if request.Message.DecodeBudget != nil {
				decoded, err = mtproto.DecompressGzipWithBudget(wrapper.PackedData, request.Message.DecodeBudget)
			} else {
				decoded, err = mtproto.DecompressGzip(wrapper.PackedData, config.MaxDecodedPayload)
			}
			if err != nil {
				return Request{}, nil, err
			}
			nested = decoded

		case mtprototl.InvokeWithLayerID:
			wrapper := &mtprototl.InvokeWithLayer{}
			if err := decodeControlBudget(body, wrapper, request.Message.DecodeBudget); err != nil {
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
			if err := decodeControlBudget(body, wrapper, request.Message.DecodeBudget); err != nil {
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
			if depth != 0 {
				return Request{}, nil, ErrInvalidInvokeAfter
			}
			wrapper := &mtprototl.InvokeAfterMsg{}
			if err := decodeControlBudget(body, wrapper, request.Message.DecodeBudget); err != nil {
				return Request{}, nil, err
			}
			if err := setInvokeAfterDependencies(&request.Message, []int64{wrapper.MsgID}); err != nil {
				return Request{}, nil, err
			}
			nested = wrapper.QueryRaw

		case mtprototl.InvokeAfterMsgsID:
			if depth != 0 {
				return Request{}, nil, ErrInvalidInvokeAfter
			}
			wrapper := &mtprototl.InvokeAfterMsgs{}
			if err := decodeControlBudget(body, wrapper, request.Message.DecodeBudget); err != nil {
				return Request{}, nil, err
			}
			if err := setInvokeAfterDependencies(&request.Message, wrapper.MsgIDs); err != nil {
				return Request{}, nil, err
			}
			nested = wrapper.QueryRaw

		case mtprototl.InvokeWithoutUpdatesID:
			wrapper := &mtprototl.InvokeWithoutUpdates{}
			if err := decodeControlBudget(body, wrapper, request.Message.DecodeBudget); err != nil {
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

func runtimeWrapperConstructor(constructorID uint32) bool {
	switch constructorID {
	case mtprototl.GzipPackedID, mtprototl.InvokeWithLayerID, mtprototl.InitConnectionID,
		mtprototl.InvokeAfterMsgID, mtprototl.InvokeAfterMsgsID, mtprototl.InvokeWithoutUpdatesID:
		return true
	default:
		return false
	}
}

func setInvokeAfterDependencies(message *InboundMessage, dependencies []int64) error {
	if message == nil || len(dependencies) == 0 || len(dependencies) > MaxInvokeAfterDependencies {
		return ErrInvalidInvokeAfter
	}
	seen := make(map[int64]struct{}, len(dependencies))
	message.Dependencies = make([]int64, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency == 0 || dependency >= message.MessageID {
			return ErrInvalidInvokeAfter
		}
		if _, duplicate := seen[dependency]; duplicate {
			continue
		}
		seen[dependency] = struct{}{}
		message.Dependencies = append(message.Dependencies, dependency)
	}
	if len(message.Dependencies) == 0 {
		return ErrInvalidInvokeAfter
	}
	return nil
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
