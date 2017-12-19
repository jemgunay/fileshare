$(document).ready(function() {
    resetInputs();

    // login form submit
    $("#login-form").submit(function(e) {
        e.preventDefault();

        var data = $(this).serialize();

        $("#login-btn").attr("disabled", true);
        $("#login-btn strong").hide();
        $("#login-btn span").show();

        performRequest(hostname + "/login", "post", data, function(result) {
            result = result.trim();

            if (result === "unauthorised") {
                setAlertWindow("warning", "Incorrect email address or password.", "#error-window");
                resetInputs();
            }
            else if (result === "error") {
                setAlertWindow("danger", "A server error occurred.", "#error-window");
                resetInputs();
            }
            else {
                window.location = "/";
            }
        });
    })
});

// Reset login inputs.
function resetInputs() {
    $("#password-input").val("");
    $("#login-btn").attr("disabled", false);
    $("#login-btn strong").show();
    $("#login-btn span").hide();
}