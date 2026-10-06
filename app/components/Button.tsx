import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { color, radius, space, type } from '../theme/tokens';

export interface ButtonProps {
  label: string;
  onPress: () => void;
  variant?: 'primary' | 'secondary' | 'ghost';
  loading?: boolean;
  disabled?: boolean;
  fullWidth?: boolean;
}

/**
 * 按鈕文字永遠是動作本身的說法（「發布協尋」而不是「發布協尋 →」），
 * 按下去會做什麼就寫什麼，不加裝飾性箭頭或驚嘆號。
 */
export function Button({ label, onPress, variant = 'primary', loading, disabled, fullWidth }: ButtonProps) {
  const isPrimary = variant === 'primary';
  const isSecondary = variant === 'secondary';

  return (
    <Pressable
      onPress={onPress}
      disabled={disabled || loading}
      style={({ pressed }) => [
        styles.base,
        isPrimary && styles.primary,
        isSecondary && styles.secondary,
        variant === 'ghost' && styles.ghost,
        fullWidth && styles.fullWidth,
        pressed && !disabled && styles.pressed,
        disabled && styles.disabled,
      ]}
    >
      {loading ? (
        <ActivityIndicator color={isPrimary ? color.white : color.home} />
      ) : (
        <View style={styles.content}>
          <Text style={[styles.label, isPrimary && styles.labelOnPrimary, variant === 'ghost' && styles.labelGhost]}>
            {label}
          </Text>
        </View>
      )}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  base: {
    borderRadius: radius.pill,
    paddingVertical: space.md + 2,
    paddingHorizontal: space.xl,
    alignItems: 'center',
    justifyContent: 'center',
  },
  fullWidth: { alignSelf: 'stretch' },
  primary: { backgroundColor: color.home },
  secondary: {
    backgroundColor: color.surface,
    borderWidth: 1.5,
    borderColor: color.home,
  },
  ghost: { backgroundColor: 'transparent', paddingHorizontal: space.sm },
  pressed: { opacity: 0.85 },
  disabled: { opacity: 0.45 },
  content: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  label: { ...type.button, color: color.home },
  labelOnPrimary: { color: color.white },
  labelGhost: { color: color.inkSoft },
});
