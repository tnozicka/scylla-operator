// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"
	"strings"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/scylladb/scylla-manager/v3/swagger/gen/scylla/v1/models"
)

// StorageProxyReloadTriggerClassesPostReader is a Reader for the StorageProxyReloadTriggerClassesPost structure.
type StorageProxyReloadTriggerClassesPostReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *StorageProxyReloadTriggerClassesPostReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewStorageProxyReloadTriggerClassesPostOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	default:
		result := NewStorageProxyReloadTriggerClassesPostDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		if response.Code()/100 == 2 {
			return result, nil
		}
		return nil, result
	}
}

// NewStorageProxyReloadTriggerClassesPostOK creates a StorageProxyReloadTriggerClassesPostOK with default headers values
func NewStorageProxyReloadTriggerClassesPostOK() *StorageProxyReloadTriggerClassesPostOK {
	return &StorageProxyReloadTriggerClassesPostOK{}
}

/*
StorageProxyReloadTriggerClassesPostOK handles this case with default header values.

Success
*/
type StorageProxyReloadTriggerClassesPostOK struct {
}

func (o *StorageProxyReloadTriggerClassesPostOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewStorageProxyReloadTriggerClassesPostDefault creates a StorageProxyReloadTriggerClassesPostDefault with default headers values
func NewStorageProxyReloadTriggerClassesPostDefault(code int) *StorageProxyReloadTriggerClassesPostDefault {
	return &StorageProxyReloadTriggerClassesPostDefault{
		_statusCode: code,
	}
}

/*
StorageProxyReloadTriggerClassesPostDefault handles this case with default header values.

internal server error
*/
type StorageProxyReloadTriggerClassesPostDefault struct {
	_statusCode int

	Payload *models.ErrorModel
}

// Code gets the status code for the storage proxy reload trigger classes post default response
func (o *StorageProxyReloadTriggerClassesPostDefault) Code() int {
	return o._statusCode
}

func (o *StorageProxyReloadTriggerClassesPostDefault) GetPayload() *models.ErrorModel {
	return o.Payload
}

func (o *StorageProxyReloadTriggerClassesPostDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.ErrorModel)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

func (o *StorageProxyReloadTriggerClassesPostDefault) Error() string {
	return fmt.Sprintf("agent [HTTP %d] %s", o._statusCode, strings.TrimRight(o.Payload.Message, "."))
}