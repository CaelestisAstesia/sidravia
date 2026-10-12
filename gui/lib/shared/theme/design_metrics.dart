/// Approved frontend dimensions. Responsive and homepage/tray exceptions stay local.
abstract final class DesignMetrics {
  static const titleFont = 20.0;
  static const dialogTitleFont = 18.0;
  static const bodyFont = 13.0;
  static const valueFont = 12.0;
  static const captionFont = 11.0;
  static const rowPadding = 12.0;
  static const rowMinHeight = 56.0;
  static const subtitleGap = 3.0;
  static const groupRadius = 12.0;
  static const buttonMinHeight = 42.0;
  static const dialogWidth = 360.0;
  static const dialogMaxHeightRatio = .8;
  static const choiceMinWidth = 92.0;
  static const choiceMaxWidth = 180.0;
  static const choiceMinHeight = 30.0;
  static const choiceRadius = 7.0;
  static const touchTarget = 48.0;

  static double choiceWidth(double viewportWidth) =>
      ((viewportWidth - 74) * .55).clamp(choiceMinWidth, choiceMaxWidth);
}
