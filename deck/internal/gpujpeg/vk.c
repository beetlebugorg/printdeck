//go:build vulkan

#include "vk.h"

#include <stdlib.h>
#include <string.h>
#include <vulkan/vulkan.h>

typedef struct {
    VkBuffer buf;
    VkDeviceMemory mem;
    void *map;
    VkDeviceSize size;
} gj_buf;

struct gj_ctx {
    VkInstance inst;
    VkPhysicalDevice phys;
    VkDevice dev;
    VkQueue queue;
    uint32_t qfam;
    VkShaderModule shader;
    VkDescriptorSetLayout dsl;
    VkPipelineLayout layout;
    VkPipeline pipe;
    VkDescriptorPool pool;
    VkDescriptorSet set;
    VkCommandPool cpool;
    VkCommandBuffer cmd;
    VkFence fence;
    gj_buf bufs[4]; // blocks, words, quant, out
};

// find_mem returns a memory type with the flags want, and with prefer if one has it, or -1.
static int find_mem(gj_ctx *c, uint32_t bits, VkMemoryPropertyFlags want, VkMemoryPropertyFlags prefer) {
    VkPhysicalDeviceMemoryProperties p;
    vkGetPhysicalDeviceMemoryProperties(c->phys, &p);
    int found = -1;
    for (uint32_t i = 0; i < p.memoryTypeCount; i++) {
        VkMemoryPropertyFlags f = p.memoryTypes[i].propertyFlags;
        if (!(bits & (1u << i)) || (f & want) != want) {
            continue;
        }
        if ((f & prefer) == prefer) {
            return (int)i;
        }
        if (found < 0) {
            found = (int)i;
        }
    }
    return found;
}

static void free_buf(gj_ctx *c, gj_buf *b) {
    if (b->map) {
        vkUnmapMemory(c->dev, b->mem);
    }
    if (b->buf) {
        vkDestroyBuffer(c->dev, b->buf, NULL);
    }
    if (b->mem) {
        vkFreeMemory(c->dev, b->mem, NULL);
    }
    memset(b, 0, sizeof *b);
}

// grow makes the buffer i size bytes or larger, and writes it in the descriptor set.
static int grow(gj_ctx *c, int i, VkDeviceSize size) {
    gj_buf *b = &c->bufs[i];
    if (size < 256) {
        size = 256;
    }
    if (b->buf && b->size >= size) {
        return 0;
    }
    vkDeviceWaitIdle(c->dev);
    free_buf(c, b);
    size += size / 4; // room for the next pictures
    VkBufferCreateInfo bi = {VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
    bi.size = size;
    bi.usage = VK_BUFFER_USAGE_STORAGE_BUFFER_BIT;
    bi.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    if (vkCreateBuffer(c->dev, &bi, NULL, &b->buf) != VK_SUCCESS) {
        return -10;
    }
    VkMemoryRequirements req;
    vkGetBufferMemoryRequirements(c->dev, b->buf, &req);
    VkMemoryPropertyFlags want = VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT;
    // The CPU reads the output: cached memory reads much faster.
    VkMemoryPropertyFlags prefer = want | (i == 3 ? VK_MEMORY_PROPERTY_HOST_CACHED_BIT : 0);
    int mt = find_mem(c, req.memoryTypeBits, want, prefer);
    if (mt < 0) {
        return -11;
    }
    VkMemoryAllocateInfo ai = {VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO};
    ai.allocationSize = req.size;
    ai.memoryTypeIndex = (uint32_t)mt;
    if (vkAllocateMemory(c->dev, &ai, NULL, &b->mem) != VK_SUCCESS) {
        return -12;
    }
    vkBindBufferMemory(c->dev, b->buf, b->mem, 0);
    if (vkMapMemory(c->dev, b->mem, 0, VK_WHOLE_SIZE, 0, &b->map) != VK_SUCCESS) {
        return -13;
    }
    b->size = size;
    VkDescriptorBufferInfo dbi = {b->buf, 0, VK_WHOLE_SIZE};
    VkWriteDescriptorSet w = {VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET};
    w.dstSet = c->set;
    w.dstBinding = (uint32_t)i;
    w.descriptorCount = 1;
    w.descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER;
    w.pBufferInfo = &dbi;
    vkUpdateDescriptorSets(c->dev, 1, &w, 0, NULL);
    return 0;
}

int gj_init(const uint32_t *spv, size_t spv_len, gj_ctx **out, char *name, size_t name_len) {
    gj_ctx *c = calloc(1, sizeof *c);
    if (!c) {
        return -1;
    }
    VkApplicationInfo app = {VK_STRUCTURE_TYPE_APPLICATION_INFO};
    app.pApplicationName = "printdeck-gpujpeg";
    app.apiVersion = VK_API_VERSION_1_1;
    VkInstanceCreateInfo ici = {VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO};
    ici.pApplicationInfo = &app;
    if (vkCreateInstance(&ici, NULL, &c->inst) != VK_SUCCESS) {
        free(c);
        return -2;
    }
    uint32_t n = 0;
    vkEnumeratePhysicalDevices(c->inst, &n, NULL);
    VkPhysicalDevice devs[16];
    if (n > 16) {
        n = 16;
    }
    vkEnumeratePhysicalDevices(c->inst, &n, devs);
    int ok = 0;
    for (uint32_t i = 0; i < n && !ok; i++) {
        uint32_t nq = 0;
        vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &nq, NULL);
        VkQueueFamilyProperties qp[16];
        if (nq > 16) {
            nq = 16;
        }
        vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &nq, qp);
        for (uint32_t q = 0; q < nq; q++) {
            if (qp[q].queueFlags & VK_QUEUE_COMPUTE_BIT) {
                c->phys = devs[i];
                c->qfam = q;
                ok = 1;
                break;
            }
        }
    }
    if (!ok) {
        gj_free(c);
        return -3;
    }
    VkPhysicalDeviceProperties props;
    vkGetPhysicalDeviceProperties(c->phys, &props);
    if (name && name_len) {
        strncpy(name, props.deviceName, name_len - 1);
        name[name_len - 1] = 0;
    }
    float prio = 1;
    VkDeviceQueueCreateInfo qci = {VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO};
    qci.queueFamilyIndex = c->qfam;
    qci.queueCount = 1;
    qci.pQueuePriorities = &prio;
    VkDeviceCreateInfo dci = {VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO};
    dci.queueCreateInfoCount = 1;
    dci.pQueueCreateInfos = &qci;
    if (vkCreateDevice(c->phys, &dci, NULL, &c->dev) != VK_SUCCESS) {
        gj_free(c);
        return -4;
    }
    vkGetDeviceQueue(c->dev, c->qfam, 0, &c->queue);

    VkShaderModuleCreateInfo smi = {VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO};
    smi.codeSize = spv_len;
    smi.pCode = spv;
    if (vkCreateShaderModule(c->dev, &smi, NULL, &c->shader) != VK_SUCCESS) {
        gj_free(c);
        return -5;
    }
    VkDescriptorSetLayoutBinding lb[4];
    for (int i = 0; i < 4; i++) {
        VkDescriptorSetLayoutBinding b = {(uint32_t)i, VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, 1, VK_SHADER_STAGE_COMPUTE_BIT, NULL};
        lb[i] = b;
    }
    VkDescriptorSetLayoutCreateInfo dli = {VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO};
    dli.bindingCount = 4;
    dli.pBindings = lb;
    vkCreateDescriptorSetLayout(c->dev, &dli, NULL, &c->dsl);
    VkPushConstantRange pcr = {VK_SHADER_STAGE_COMPUTE_BIT, 0, 16};
    VkPipelineLayoutCreateInfo pli = {VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO};
    pli.setLayoutCount = 1;
    pli.pSetLayouts = &c->dsl;
    pli.pushConstantRangeCount = 1;
    pli.pPushConstantRanges = &pcr;
    vkCreatePipelineLayout(c->dev, &pli, NULL, &c->layout);
    VkComputePipelineCreateInfo cpi = {VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO};
    cpi.stage.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
    cpi.stage.stage = VK_SHADER_STAGE_COMPUTE_BIT;
    cpi.stage.module = c->shader;
    cpi.stage.pName = "main";
    cpi.layout = c->layout;
    if (vkCreateComputePipelines(c->dev, VK_NULL_HANDLE, 1, &cpi, NULL, &c->pipe) != VK_SUCCESS) {
        gj_free(c);
        return -6;
    }
    VkDescriptorPoolSize ps = {VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, 4};
    VkDescriptorPoolCreateInfo dpi = {VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO};
    dpi.maxSets = 1;
    dpi.poolSizeCount = 1;
    dpi.pPoolSizes = &ps;
    vkCreateDescriptorPool(c->dev, &dpi, NULL, &c->pool);
    VkDescriptorSetAllocateInfo dsa = {VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO};
    dsa.descriptorPool = c->pool;
    dsa.descriptorSetCount = 1;
    dsa.pSetLayouts = &c->dsl;
    vkAllocateDescriptorSets(c->dev, &dsa, &c->set);
    VkCommandPoolCreateInfo cpci = {VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO};
    cpci.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT;
    cpci.queueFamilyIndex = c->qfam;
    vkCreateCommandPool(c->dev, &cpci, NULL, &c->cpool);
    VkCommandBufferAllocateInfo cba = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO};
    cba.commandPool = c->cpool;
    cba.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
    cba.commandBufferCount = 1;
    vkAllocateCommandBuffers(c->dev, &cba, &c->cmd);
    VkFenceCreateInfo fci = {VK_STRUCTURE_TYPE_FENCE_CREATE_INFO};
    vkCreateFence(c->dev, &fci, NULL, &c->fence);
    int rc = grow(c, 2, 5 * 64 * sizeof(int32_t));
    if (rc) {
        gj_free(c);
        return rc;
    }
    *out = c;
    return 0;
}

void gj_free(gj_ctx *c) {
    if (!c) {
        return;
    }
    if (c->dev) {
        vkDeviceWaitIdle(c->dev);
        for (int i = 0; i < 4; i++) {
            free_buf(c, &c->bufs[i]);
        }
        if (c->fence) vkDestroyFence(c->dev, c->fence, NULL);
        if (c->cpool) vkDestroyCommandPool(c->dev, c->cpool, NULL);
        if (c->pool) vkDestroyDescriptorPool(c->dev, c->pool, NULL);
        if (c->pipe) vkDestroyPipeline(c->dev, c->pipe, NULL);
        if (c->layout) vkDestroyPipelineLayout(c->dev, c->layout, NULL);
        if (c->dsl) vkDestroyDescriptorSetLayout(c->dev, c->dsl, NULL);
        if (c->shader) vkDestroyShaderModule(c->dev, c->shader, NULL);
        vkDestroyDevice(c->dev, NULL);
    }
    if (c->inst) {
        vkDestroyInstance(c->inst, NULL);
    }
    free(c);
}

int gj_reserve(gj_ctx *c, size_t n_blocks, size_t n_words, size_t out_len) {
    int rc = grow(c, 0, n_blocks * 16);
    if (!rc) rc = grow(c, 1, n_words * 4);
    if (!rc) rc = grow(c, 3, out_len);
    return rc;
}

void gj_put_blocks(gj_ctx *c, const uint32_t *blocks, size_t n_blocks) {
    memcpy(c->bufs[0].map, blocks, n_blocks * 16);
}

void gj_put_words(gj_ctx *c, size_t at, const uint32_t *words, size_t n) {
    memcpy((uint32_t *)c->bufs[1].map + at, words, n * 4);
}

void gj_put_quant(gj_ctx *c, const int32_t *quant) {
    memcpy(c->bufs[2].map, quant, 5 * 64 * sizeof(int32_t));
}

int gj_run(gj_ctx *c, uint32_t n_blocks, const uint32_t strides[3]) {
    uint32_t push[4] = {n_blocks, strides[0], strides[1], strides[2]};
    vkResetCommandBuffer(c->cmd, 0);
    VkCommandBufferBeginInfo bi = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO};
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(c->cmd, &bi);
    vkCmdBindPipeline(c->cmd, VK_PIPELINE_BIND_POINT_COMPUTE, c->pipe);
    vkCmdBindDescriptorSets(c->cmd, VK_PIPELINE_BIND_POINT_COMPUTE, c->layout, 0, 1, &c->set, 0, NULL);
    vkCmdPushConstants(c->cmd, c->layout, VK_SHADER_STAGE_COMPUTE_BIT, 0, sizeof push, push);
    vkCmdDispatch(c->cmd, (n_blocks + 7) / 8, 1, 1);
    // The CPU reads the output after the shader.
    VkMemoryBarrier mb = {VK_STRUCTURE_TYPE_MEMORY_BARRIER};
    mb.srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT;
    mb.dstAccessMask = VK_ACCESS_HOST_READ_BIT;
    vkCmdPipelineBarrier(c->cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, VK_PIPELINE_STAGE_HOST_BIT, 0, 1, &mb, 0, NULL, 0, NULL);
    vkEndCommandBuffer(c->cmd);
    VkSubmitInfo si = {VK_STRUCTURE_TYPE_SUBMIT_INFO};
    si.commandBufferCount = 1;
    si.pCommandBuffers = &c->cmd;
    vkResetFences(c->dev, 1, &c->fence);
    if (vkQueueSubmit(c->queue, 1, &si, c->fence) != VK_SUCCESS) {
        return -20;
    }
    if (vkWaitForFences(c->dev, 1, &c->fence, VK_TRUE, 5000000000ull) != VK_SUCCESS) {
        return -21;
    }
    return 0;
}

const uint8_t *gj_out(gj_ctx *c) { return c->bufs[3].map; }
