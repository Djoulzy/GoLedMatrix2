//go:build cgo && rpistub

package display

/*
// This test-only native API stub uses the actual pinned header. The production
// CGO binding runs unchanged, but never opens GPIO or links the hardware library.
#include <led-matrix-c.h>
#include <stdlib.h>
#include <string.h>

struct LedCanvas {
  int width, height;
  uint8_t *pixels;
};
struct RGBLedMatrix {
  struct LedCanvas *front, *initial, *offscreen;
};

static int stub_fail;
int stub_created, stub_deleted, stub_swaps, stub_writes;
int stub_active_writes, stub_invalid_images;
static struct RGBLedMatrix *stub_matrix;
struct RGBLedMatrixOptions stub_options;
struct RGBLedRuntimeOptions stub_runtime;
char stub_mapping[128], stub_mapper[128], stub_sequence[16];

static struct LedCanvas *stub_canvas(int width, int height) {
  struct LedCanvas *c = calloc(1, sizeof(*c));
  c->width = width; c->height = height;
  if (width > 0 && height > 0) c->pixels = calloc(3 * width * height, 1);
  return c;
}

struct RGBLedMatrix *led_matrix_create_from_options_and_rt_options(
  struct RGBLedMatrixOptions *options, struct RGBLedRuntimeOptions *runtime) {
  if (stub_fail == 1) return NULL;
  stub_options = *options; stub_runtime = *runtime;
  snprintf(stub_mapping, sizeof(stub_mapping), "%s", options->hardware_mapping);
  snprintf(stub_mapper, sizeof(stub_mapper), "%s", options->pixel_mapper_config);
  snprintf(stub_sequence, sizeof(stub_sequence), "%s", options->led_rgb_sequence);
  struct RGBLedMatrix *m = calloc(1, sizeof(*m));
  // Return a geometry different from the config to exercise mapper handling.
  m->initial = stub_canvas(3, stub_fail == 3 ? 0 : 2);
  m->front = m->initial;
  stub_matrix = m;
  ++stub_created;
  return m;
}

struct LedCanvas *led_matrix_create_offscreen_canvas(struct RGBLedMatrix *m) {
  if (stub_fail == 2) return NULL;
  m->offscreen = stub_canvas(m->front->width, m->front->height);
  return m->offscreen;
}

void led_canvas_get_size(const struct LedCanvas *c, int *width, int *height) {
  *width = c->width; *height = c->height;
}

void set_image(struct LedCanvas *c, int x, int y, const uint8_t *pixels,
               size_t size, int width, int height, char is_bgr) {
  ++stub_writes;
  if (c == stub_matrix->front) ++stub_active_writes;
  if (x != 0 || y != 0 || width != c->width || height != c->height ||
      size != (size_t)(3 * width * height) || is_bgr) {
    ++stub_invalid_images;
    return;
  }
  memcpy(c->pixels, pixels, size);
}

struct LedCanvas *led_matrix_swap_on_vsync(struct RGBLedMatrix *m, struct LedCanvas *c) {
  struct LedCanvas *previous = m->front;
  m->front = c;
  ++stub_swaps;
  return previous;
}

void led_matrix_delete(struct RGBLedMatrix *m) {
  free(m->initial->pixels); free(m->initial);
  if (m->offscreen) { free(m->offscreen->pixels); free(m->offscreen); }
  free(m);
  stub_matrix = NULL;
  ++stub_deleted;
}

static void stub_reset(int failure) {
  stub_fail = failure;
  stub_created = stub_deleted = stub_swaps = stub_writes = 0;
  stub_active_writes = stub_invalid_images = 0;
}
static int stub_pixel(int offset) { return stub_matrix->front->pixels[offset]; }
*/
import "C"

func resetRPIStub(failure int) { C.stub_reset(C.int(failure)) }

func rpiStubCounts() (created, deleted, swaps, writes, activeWrites, invalidImages int) {
	return int(C.stub_created), int(C.stub_deleted), int(C.stub_swaps), int(C.stub_writes), int(C.stub_active_writes), int(C.stub_invalid_images)
}

func rpiStubPixels() []byte {
	pixels := make([]byte, 3*3*2)
	for i := range pixels {
		pixels[i] = byte(C.stub_pixel(C.int(i)))
	}
	return pixels
}

func rpiStubConfig() RPIConfig {
	o, rt := C.stub_options, C.stub_runtime
	return RPIConfig{
		HardwareMapping: C.GoString(&C.stub_mapping[0]), PixelMapperConfig: C.GoString(&C.stub_mapper[0]), RGBSequence: C.GoString(&C.stub_sequence[0]),
		Rows: int(o.rows), Cols: int(o.cols), ChainLength: int(o.chain_length), Parallel: int(o.parallel),
		Multiplexing: int(o.multiplexing), Brightness: int(o.brightness), PWMBits: int(o.pwm_bits),
		ShowRefreshRate: bool(o.show_refresh_rate), LimitRefreshRateHz: int(o.limit_refresh_rate_hz),
		ScanMode: int(o.scan_mode), RowAddressType: int(o.row_address_type), PWMLSBNanoseconds: int(o.pwm_lsb_nanoseconds),
		PWMDitherBits: int(o.pwm_dither_bits), DisableHardwarePulsing: bool(o.disable_hardware_pulsing), InverseColors: bool(o.inverse_colors),
		GPIOSlowdown: int(rt.gpio_slowdown),
	}
}
