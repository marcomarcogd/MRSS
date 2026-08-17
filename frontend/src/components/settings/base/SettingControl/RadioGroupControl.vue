<script setup lang="ts">
interface RadioOption {
  value: string;
  label: string;
  description?: string;
}

interface Props {
  modelValue: string;
  options: RadioOption[];
  name: string;
  disabled?: boolean;
}

defineProps<Props>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();
</script>

<template>
  <div class="radio-group" role="radiogroup">
    <label
      v-for="option in options"
      :key="option.value"
      class="radio-option"
      :class="{ 'radio-option-active': modelValue === option.value }"
    >
      <input
        class="radio-input"
        type="radio"
        :name="name"
        :value="option.value"
        :checked="modelValue === option.value"
        :disabled="disabled"
        @change="emit('update:modelValue', option.value)"
      />
      <span class="radio-copy">
        <span class="radio-label">{{ option.label }}</span>
        <span v-if="option.description" class="radio-description">{{ option.description }}</span>
      </span>
    </label>
  </div>
</template>

<style scoped>
@reference "../../../../style.css";
.radio-group {
  @apply grid gap-2 w-full;
}

.radio-option {
  @apply flex items-start gap-3 rounded-md border border-border bg-bg-tertiary px-3 py-2.5 cursor-pointer transition-colors hover:bg-bg-secondary;
}

.radio-option-active {
  @apply border-accent bg-accent/5;
}

.radio-input {
  @apply mt-0.5 h-4 w-4 shrink-0 accent-accent cursor-pointer disabled:cursor-not-allowed;
}

.radio-copy {
  @apply flex min-w-0 flex-col gap-0.5;
}

.radio-label {
  @apply text-sm font-medium text-text-primary;
}

.radio-description {
  @apply text-xs leading-5 text-text-secondary;
}

.radio-option:has(.radio-input:disabled) {
  @apply opacity-50 cursor-not-allowed;
}
</style>
