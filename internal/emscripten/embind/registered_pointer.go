package embind

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
)

type registeredPointerType struct {
	baseType
	registeredClass *classType
	constructors    map[int32]publicSymbolFn
	isReference     bool
	isConst         bool

	// smart pointer properties
	isSmartPointer bool
	pointeeType    any
	sharingPolicy  any
	rawGetPointee  any
	rawConstructor any
	rawShare       any
	rawDestructor  any
}

func (rpt *registeredPointerType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	/*
	  function getBasestPointer(class_, ptr) {
	      if (ptr === undefined) {
	          throwBindingError('ptr should not be undefined');
	      }
	      while (class_.baseClass) {
	          ptr = class_.upcast(ptr);
	          class_ = class_.baseClass;
	      }
	      return ptr;
	    }
	  function getInheritedInstance(class_, ptr) {
	      ptr = getBasestPointer(class_, ptr);
	      return registeredInstances[ptr];
	    }


	  function makeClassHandle(prototype, record) {
	      if (!record.ptrType || !record.ptr) {
	        throwInternalError('makeClassHandle requires ptr and ptrType');
	      }
	      var hasSmartPtrType = !!record.smartPtrType;
	      var hasSmartPtr = !!record.smartPtr;
	      if (hasSmartPtrType !== hasSmartPtr) {
	        throwInternalError('Both smartPtrType and smartPtr must be specified');
	      }
	      record.count = { value: 1 };
	      return attachFinalizer(Object.create(prototype, {
	        $$: {
	            value: record,
	        },
	      }));
	    }
	  function RegisteredPointer_fromWireType(ptr) {
	      // ptr is a raw pointer (or a raw smartpointer)

	      // rawPointer is a maybe-null raw pointer
	      var rawPointer = this.getPointee(ptr);
	      if (!rawPointer) {
	        this.destructor(ptr);
	        return null;
	      }

	      var registeredInstance = getInheritedInstance(this.registeredClass, rawPointer);
	      if (undefined !== registeredInstance) {
	        // JS object has been neutered, time to repopulate it
	        if (0 === registeredInstance.$$.count.value) {
	          registeredInstance.$$.ptr = rawPointer;
	          registeredInstance.$$.smartPtr = ptr;
	          return registeredInstance['clone']();
	        } else {
	          // else, just increment reference count on existing object
	          // it already has a reference to the smart pointer
	          var rv = registeredInstance['clone']();
	          this.destructor(ptr);
	          return rv;
	        }
	      }

	      function makeDefaultHandle() {
	        if (this.isSmartPointer) {
	          return makeClassHandle(this.registeredClass.instancePrototype, {
	            ptrType: this.pointeeType,
	            ptr: rawPointer,
	            smartPtrType: this,
	            smartPtr: ptr,
	          });
	        } else {
	          return makeClassHandle(this.registeredClass.instancePrototype, {
	            ptrType: this,
	            ptr,
	          });
	        }
	      }

	      var actualType = this.registeredClass.getActualType(rawPointer);
	      var registeredPointerRecord = registeredPointers[actualType];
	      if (!registeredPointerRecord) {
	        return makeDefaultHandle.call(this);
	      }

	      var toType;
	      if (this.isConst) {
	        toType = registeredPointerRecord.constPointerType;
	      } else {
	        toType = registeredPointerRecord.pointerType;
	      }
	      var dp = downcastPointer(
	          rawPointer,
	          this.registeredClass,
	          toType.registeredClass);
	      if (dp === null) {
	        return makeDefaultHandle.call(this);
	      }
	      if (this.isSmartPointer) {
	        return makeClassHandle(toType.registeredClass.instancePrototype, {
	          ptrType: toType,
	          ptr: dp,
	          smartPtrType: this,
	          smartPtr: ptr,
	        });
	      } else {
	        return makeClassHandle(toType.registeredClass.instancePrototype, {
	          ptrType: toType,
	          ptr: dp,
	        });
	      }
	    }
	  var attachFinalizer = function(handle) {
	      if ('undefined' === typeof FinalizationRegistry) {
	        attachFinalizer = (handle) => handle;
	        return handle;
	      }
	      // If the running environment has a FinalizationRegistry (see
	      // https://github.com/tc39/proposal-weakrefs), then attach finalizers
	      // for class handles.  We check for the presence of FinalizationRegistry
	      // at run-time, not build-time.
	      finalizationRegistry = new FinalizationRegistry((info) => {
	        console.warn(info.leakWarning.stack.replace(/^Error: /, ''));
	        releaseClassHandle(info.$$);
	      });
	      attachFinalizer = (handle) => {
	        var $$ = handle.$$;
	        var hasSmartPtr = !!$$.smartPtr;
	        if (hasSmartPtr) {
	          // We should not call the destructor on raw pointers in case other code expects the pointee to live
	          var info = { $$: $$ };
	          // Create a warning as an Error instance in advance so that we can store
	          // the current stacktrace and point to it when / if a leak is detected.
	          // This is more useful than the empty stacktrace of `FinalizationRegistry`
	          // callback.
	          var cls = $$.ptrType.registeredClass;
	          info.leakWarning = new Error(`Embind found a leaked C++ instance ${cls.name} <${ptrToString($$.ptr)}>.\n` +
	          "We'll free it automatically in this case, but this functionality is not reliable across various environments.\n" +
	          "Make sure to invoke .delete() manually once you're done with the instance instead.\n" +
	          "Originally allocated"); // `.stack` will add "at ..." after this sentence
	          if ('captureStackTrace' in Error) {
	            Error.captureStackTrace(info.leakWarning, RegisteredPointer_fromWireType);
	          }
	          finalizationRegistry.register(handle, info, handle);
	        }
	        return handle;
	      };
	      detachFinalizer = (handle) => finalizationRegistry.unregister(handle);
	      return attachFinalizer(handle);
	    };
	*/
	return 0, fmt.Errorf("unknown registered pointer fromWireType")
}

func (rpt *registeredPointerType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	return 0, fmt.Errorf("unknown registered pointer toWireType")
}

func (rpt *registeredPointerType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	value, ok := mod.Memory().ReadUint32Le(pointer)
	if !ok {
		return nil, fmt.Errorf("could not read register pointer value at pointer %d", pointer)
	}
	return rpt.FromWireType(ctx, mod, api.EncodeU32(value))
}

func (rpt *registeredPointerType) HasDestructorFunction() bool {
	return true
}

func (rpt *registeredPointerType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) error {
	/*
	   if (this.rawDestructor) {
	     this.rawDestructor(ptr);
	   }
	*/
	return nil
}

func (rpt *registeredPointerType) HasDeleteObject() bool {
	return true
}

func (rpt *registeredPointerType) DeleteObject(ctx context.Context, mod api.Module, handle any) error {
	/*
	   if (handle !== null) {
	     handle['delete']();
	   }
	*/
	return nil
}

func (rpt *registeredPointerType) GetPointee() bool {
	/*
	   if (this.rawGetPointee) {
	     ptr = this.rawGetPointee(ptr);
	   }
	   return ptr;
	*/
	return true
}
